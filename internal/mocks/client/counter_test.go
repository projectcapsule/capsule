// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package mock_client

import (
	"sync"
	"testing"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestCallCounterForwardsAndCounts(t *testing.T) {
	t.Parallel()
	calls := &CallCounter{}
	c := fake.NewClientBuilder().WithStatusSubresource(&corev1.Pod{}).WithInterceptorFuncs(calls.Interceptors()).Build()
	ctx := t.Context()
	pod := &corev1.Pod{Name: "pod", Namespace: "team"}
	if err := c.Create(ctx, pod); err != nil {
		t.Fatal(err)
	}
	if err := c.Get(ctx, client.ObjectKeyFromObject(pod), pod); err != nil {
		t.Fatal(err)
	}
	if err := c.List(ctx, &corev1.PodList{}, client.InNamespace("team")); err != nil {
		t.Fatal(err)
	}
	pod.Status.Phase = corev1.PodRunning
	if err := c.Status().Update(ctx, pod); err != nil {
		t.Fatal(err)
	}
	before := pod.DeepCopy()
	pod.Labels = map[string]string{"test": "yes"}
	if err := c.Patch(ctx, pod, client.MergeFrom(before)); err != nil {
		t.Fatal(err)
	}
	if err := c.Get(ctx, client.ObjectKeyFromObject(pod), pod); err != nil {
		t.Fatal(err)
	}
	if pod.Status.Phase != corev1.PodRunning || pod.Labels["test"] != "yes" {
		t.Fatal("writes were not forwarded")
	}
	if err := c.Delete(ctx, pod); err != nil {
		t.Fatal(err)
	}
	if err := c.Get(ctx, client.ObjectKeyFromObject(pod), pod); !apierrors.IsNotFound(err) {
		t.Fatalf("expected preserved NotFound, got %v", err)
	}
	if calls.gets.Load() != 3 || calls.lists.Load() != 1 || calls.writes.Load() != 4 {
		t.Fatalf("wrong counts: gets=%d lists=%d writes=%d", calls.gets.Load(), calls.lists.Load(), calls.writes.Load())
	}
	calls.Reset()
	if calls.gets.Load()+calls.lists.Load()+calls.writes.Load() != 0 {
		t.Fatal("reset did not clear counts")
	}
}

func TestCallCounterConcurrentReads(t *testing.T) {
	t.Parallel()
	calls := &CallCounter{}
	c := fake.NewClientBuilder().WithObjects(&corev1.ConfigMap{Name: "config", Namespace: "team"}).WithInterceptorFuncs(calls.Interceptors()).Build()
	var workers sync.WaitGroup
	for range 8 {
		workers.Go(func() {
			for range 100 {
				if err := c.Get(t.Context(), client.ObjectKey{Name: "config", Namespace: "team"}, &corev1.ConfigMap{}); err != nil {
					t.Error(err)
				}
			}
		})
	}
	workers.Wait()
	if calls.gets.Load() != 800 {
		t.Fatalf("lost concurrent reads: %d", calls.gets.Load())
	}
}
