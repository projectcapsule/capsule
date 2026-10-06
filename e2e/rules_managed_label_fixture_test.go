// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"
	clienttesting "k8s.io/client-go/testing"
	"sigs.k8s.io/controller-runtime/pkg/client"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	apiruntime "github.com/projectcapsule/capsule/pkg/api/runtime"
)

func newManagedLabelReplication(name, tenant, namespace string) *capsulev1beta2.GlobalTenantResource {
	return &capsulev1beta2.GlobalTenantResource{Name: name, Spec: capsulev1beta2.GlobalTenantResourceSpec{
		Scope:          api.ResourceScopeNamespace,
		TenantSelector: metav1.LabelSelector{MatchLabels: map[string]string{"example.org/managed-label-tenant": tenant}},
		ServiceAccount: resourcePermitServiceAccountReference(namespace, name),
		TenantResourceCommonSpec: capsulev1beta2.TenantResourceCommonSpec{
			// A zero Duration is serialized as "0s", overriding the API default
			// and disabling retries after protection waits for parent status.
			ResyncPeriod: resyncPeriod,
			Resources: []capsulev1beta2.ResourceSpec{{
				NamespaceSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"kubernetes.io/metadata.name": namespace}},
				Policy:            &apiruntime.ResourceReplicationPolicy{Protect: new(true)},
				RawItems: []capsulev1beta2.RawExtension{{Object: &corev1.ConfigMap{
					APIVersion: "v1", Kind: "ConfigMap", Name: name, Data: map[string]string{"source": meta.ValueControllerReplications},
				}}},
			}},
		},
	}}
}

// Delete only this fixture's incarnation, using its authorized controller client.
// Tenant cleanup runs afterward with the ordinary suite client.
func deleteManagedLabelPod(ctx context.Context, controller kubernetes.Interface, pod *corev1.Pod) error {
	pods := controller.CoreV1().Pods(pod.Namespace)
	if err := pods.Delete(ctx, pod.Name, metav1.DeleteOptions{
		GracePeriodSeconds: new(int64(0)), Preconditions: &metav1.Preconditions{UID: &pod.UID},
	}); err != nil {
		return client.IgnoreNotFound(err)
	}
	_, err := pods.Get(ctx, pod.Name, metav1.GetOptions{})
	if err == nil {
		return fmt.Errorf("managed Pod %s/%s still exists", pod.Namespace, pod.Name)
	}
	return client.IgnoreNotFound(err)
}

func TestManagedLabelReplicationRetainsPeriodicReconciliation(t *testing.T) {
	fixture := newManagedLabelReplication("replication", "tenant-a", "tenant-a-selected")
	raw, err := json.Marshal(fixture)
	require.NoError(t, err)
	var persisted capsulev1beta2.GlobalTenantResource
	require.NoError(t, json.Unmarshal(raw, &persisted))
	require.Greater(t, persisted.Spec.ResyncPeriod.Duration, time.Duration(0), "0s prevents recovery from the first protection check")
	require.Less(t, persisted.Spec.ResyncPeriod.Duration, defaultTimeoutInterval)
	require.True(t, persisted.Spec.Resources[0].Policy.IsProtected(), "retry must not relax protection")
}

func TestManagedLabelPodCleanup(t *testing.T) {
	for _, state := range []string{"present", "gone", "forbidden", "terminating", "replacement"} {
		t.Run(state, func(t *testing.T) {
			pod := &corev1.Pod{Name: "managed", Namespace: "tenant-a", UID: "created-uid"}
			controller := fake.NewClientset(pod)
			if state == "gone" {
				require.NoError(t, controller.Tracker().Delete(corev1.SchemeGroupVersion.WithResource("pods"), pod.Namespace, pod.Name))
			}
			controller.PrependReactor("delete", "pods", func(action clienttesting.Action) (bool, runtime.Object, error) {
				deletion := action.(clienttesting.DeleteAction)
				require.Equal(t, pod.Namespace, deletion.GetNamespace())
				require.Equal(t, pod.Name, deletion.GetName())
				opts := deletion.GetDeleteOptions()
				require.NotNil(t, opts.GracePeriodSeconds)
				require.Zero(t, *opts.GracePeriodSeconds)
				require.NotNil(t, opts.Preconditions)
				require.Equal(t, types.UID("created-uid"), *opts.Preconditions.UID)
				switch state {
				case "forbidden":
					return true, nil, apierrors.NewForbidden(schema.GroupResource{Resource: "pods"}, pod.Name, fmt.Errorf("wrong cleanup identity"))
				case "terminating":
					return true, nil, nil
				case "replacement":
					return true, nil, apierrors.NewConflict(schema.GroupResource{Resource: "pods"}, pod.Name, fmt.Errorf("UID precondition failed"))
				}
				return false, nil, nil
			})
			err := deleteManagedLabelPod(t.Context(), controller, pod)
			switch state {
			case "present", "gone":
				require.NoError(t, err)
			case "forbidden":
				require.True(t, apierrors.IsForbidden(err))
			case "terminating":
				require.ErrorContains(t, err, "still exists")
			case "replacement":
				require.True(t, apierrors.IsConflict(err))
			}
		})
	}
}
