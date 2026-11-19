// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package replications

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	"github.com/projectcapsule/capsule/cmd/controller/common"
)

func legacySpec() corev1.PodSpec {
	return corev1.PodSpec{Containers: []corev1.Container{{Name: "manager", Args: []string{"--configuration-name=default", "--enable-leader-election=true"}}}}
}

func TestLegacyHandoffFindsEveryOldReplica(t *testing.T) {
	for _, test := range []struct {
		name string
		obj  client.Object
		want string
	}{
		{"deployment", &appsv1.Deployment{Spec: appsv1.DeploymentSpec{Replicas: new(int32(2)), Template: corev1.PodTemplateSpec{Spec: legacySpec()}}}, "Deployment/old"},
		{"scaled-down-deployment", &appsv1.Deployment{Spec: appsv1.DeploymentSpec{Replicas: new(int32(0)), Template: corev1.PodTemplateSpec{Spec: legacySpec()}}}, ""},
		{"standby-replicaset", &appsv1.ReplicaSet{Spec: appsv1.ReplicaSetSpec{Replicas: new(int32(1)), Template: corev1.PodTemplateSpec{Spec: legacySpec()}}}, "ReplicaSet/old"},
		{"scaled-down-replicaset", &appsv1.ReplicaSet{Spec: appsv1.ReplicaSetSpec{Replicas: new(int32(0)), Template: corev1.PodTemplateSpec{Spec: legacySpec()}}}, ""},
		{"daemonset", &appsv1.DaemonSet{Spec: appsv1.DaemonSetSpec{Template: corev1.PodTemplateSpec{Spec: legacySpec()}}}, "DaemonSet/old"},
		{"pending-pod", &corev1.Pod{Spec: legacySpec()}, "Pod/old"},
		{"standby-pod", &corev1.Pod{Spec: legacySpec(), Status: corev1.PodStatus{Phase: corev1.PodRunning}}, "Pod/old"},
		{"terminating-pod", &corev1.Pod{DeletionTimestamp: new(metav1.Now()), Finalizers: []string{"test/finalizer"}, Spec: legacySpec(), Status: corev1.PodStatus{Phase: corev1.PodRunning}}, "Pod/old"},
		{"terminated-manager", &corev1.Pod{DeletionTimestamp: new(metav1.Now()), Finalizers: []string{"test/finalizer"}, Spec: legacySpec(), Status: corev1.PodStatus{Phase: corev1.PodRunning, ContainerStatuses: []corev1.ContainerStatus{{Name: "manager", State: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{}}}}}}, ""},
		{"restarting-manager", &corev1.Pod{Spec: legacySpec(), Status: corev1.PodStatus{ContainerStatuses: []corev1.ContainerStatus{{Name: "manager", State: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{}}}}}}, "Pod/old"},
		{"failed-pod", &corev1.Pod{Spec: legacySpec(), Status: corev1.PodStatus{Phase: corev1.PodFailed}}, ""},
		{"completed-pod", &corev1.Pod{Spec: legacySpec(), Status: corev1.PodStatus{Phase: corev1.PodSucceeded}}, ""},
		{"another-namespace", &corev1.Pod{Namespace: "other", Spec: legacySpec()}, ""},
		{"another-release", &corev1.Pod{Labels: map[string]string{"app.kubernetes.io/instance": "other"}, Spec: legacySpec()}, ""},
		{"core-subcommand", &corev1.Pod{Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "manager", Args: []string{"core", "--configuration-name=default"}}}}}, ""},
		{"worker-subcommand", &corev1.Pod{Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "manager", Args: []string{"replications"}}}}}, ""},
		{"command-subcommand", &corev1.Pod{Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "manager", Command: []string{"/manager", "core"}, Args: []string{"--configuration-name=default"}}}}}, ""},
		{"core-flag-value-is-not-mode", &corev1.Pod{Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "manager", Args: []string{"--configuration-name", "core"}}}}}, "Pod/old"},
		{"other-container", &corev1.Pod{Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "proxy"}}}}, ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			test.obj.SetName("old")
			if test.obj.GetNamespace() == "" {
				test.obj.SetNamespace("capsule-system")
			}
			if test.obj.GetLabels() == nil {
				test.obj.SetLabels(map[string]string{"app.kubernetes.io/instance": "capsule"})
			}
			reader := fake.NewClientBuilder().WithScheme(common.NewScheme()).WithObjects(test.obj).Build()
			h := &legacyHandoff{reader: reader, namespace: "capsule-system", release: "capsule", completed: make(chan struct{})}
			blocked, err := h.blockedBy(t.Context())
			require.NoError(t, err)
			require.Equal(t, test.want, blocked)
		})
	}
}

type startedCache struct {
	cache.Cache
	started bool
	synced  bool
}

func (c *startedCache) Start(context.Context) error           { c.started = true; return nil }
func (c *startedCache) WaitForCacheSync(context.Context) bool { c.synced = true; return true }

func TestLegacyHandoffGatesCacheAndReadiness(t *testing.T) {
	for _, block := range []bool{true, false} {
		t.Run(map[bool]string{true: "old-standby", false: "fresh-install"}[block], func(t *testing.T) {
			reader := fake.NewClientBuilder().WithScheme(common.NewScheme()).Build()
			old := &corev1.Pod{Name: "old", Namespace: "capsule-system", Spec: legacySpec()}
			if block {
				require.NoError(t, reader.Create(t.Context(), old))
			}
			h := &legacyHandoff{reader: reader, namespace: "capsule-system", completed: make(chan struct{})}
			inner := &startedCache{}
			c := &handoffCache{Cache: inner, handoff: h}
			require.Error(t, h.ready(nil))
			ctx, cancel := context.WithCancel(t.Context())
			cancel()
			require.False(t, c.WaitForCacheSync(ctx), "even an empty underlying cache must not bypass handoff")
			require.False(t, inner.synced)
			if block {
				require.ErrorIs(t, c.Start(ctx), context.Canceled)
				require.False(t, inner.started)
				require.Error(t, h.ready(nil))
				require.NoError(t, reader.Delete(t.Context(), old))
			}
			require.NoError(t, c.Start(t.Context()))
			require.True(t, inner.started)
			require.True(t, c.WaitForCacheSync(t.Context()))
			require.True(t, inner.synced)
			require.NoError(t, h.ready(nil))
		})
	}
}

func TestLegacyHandoffReadErrorsDoNotPermitWorkers(t *testing.T) {
	want := errors.New("API unavailable")
	reader := fake.NewClientBuilder().WithScheme(common.NewScheme()).Build()
	h := &legacyHandoff{namespace: "capsule-system", completed: make(chan struct{}), reader: interceptor.NewClient(reader, interceptor.Funcs{List: func(context.Context, client.WithWatch, client.ObjectList, ...client.ListOption) error { return want }})}
	require.ErrorIs(t, h.wait(t.Context()), want)
	require.Error(t, h.ready(nil))
}

func TestLegacyHandoffReadinessDoesNotRepeatAPIReads(t *testing.T) {
	reader := fake.NewClientBuilder().WithScheme(common.NewScheme()).Build()
	reads := 0
	h := &legacyHandoff{namespace: "capsule-system", release: "capsule", completed: make(chan struct{}), reader: interceptor.NewClient(reader, interceptor.Funcs{List: func(ctx context.Context, c client.WithWatch, list client.ObjectList, opts ...client.ListOption) error {
		reads++
		options := (&client.ListOptions{}).ApplyOptions(opts)
		require.Equal(t, "capsule-system", options.Namespace)
		require.Equal(t, "app.kubernetes.io/instance=capsule", options.LabelSelector.String())
		return c.List(ctx, list, opts...)
	}})}
	require.NoError(t, h.wait(t.Context()))
	require.Equal(t, 4, reads)
	for range 100 {
		require.NoError(t, h.ready(nil))
	}
	require.Equal(t, 4, reads)
}

func BenchmarkLegacyHandoffReadiness(b *testing.B) {
	h := &legacyHandoff{completed: make(chan struct{})}
	close(h.completed)
	b.ReportAllocs()
	for b.Loop() {
		if err := h.ready(nil); err != nil {
			b.Fatal(err)
		}
	}
}
