// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package resources

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-logr/logr/testr"
	dto "github.com/prometheus/client_model/go"
	admissionv1 "k8s.io/api/admissionregistration/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/dynamic/dynamicinformer"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	clientcache "k8s.io/client-go/tools/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
)

func TestSharedWatchStartupInitialListAndShutdown(t *testing.T) {
	f := watchFixtureFor(t, 0)
	f.watcher.log = testr.New(t)
	dynamic := f.watcher.dynamic.(*dynamicfake.FakeDynamicClient)
	initial := &corev1.ConfigMap{Name: "source", Namespace: "a", ResourceVersion: "1", Labels: map[string]string{"source": "yes"}}
	if err := dynamic.Tracker().Add(initial); err != nil {
		t.Fatal(err)
	}
	var watchStarts atomic.Int64
	informer := dynamicinformer.NewFilteredDynamicInformer(dynamic, configMapsGVR, metav1.NamespaceAll, 0, clientcache.Indexers{}, nil).Informer()
	f.watcher.newInformer = func(schema.GroupVersionResource, string) clientcache.SharedIndexInformer {
		watchStarts.Add(1)
		return informer
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- f.watcher.Start(ctx) }()
	poll := func(phase string, condition func() bool) {
		t.Helper()
		if err := wait.PollUntilContextTimeout(t.Context(), 5*time.Millisecond, 5*time.Second, true, func(context.Context) (bool, error) { return condition(), nil }); err != nil {
			t.Fatalf("%s: %v starts=%d synced=%t queued=%d targets=%v", phase, err, watchStarts.Load(), informer.HasSynced(), len(f.watcher.events), f.notifications.take())
		}
	}
	poll("initial sync", func() bool { return informer.HasSynced() })
	if len(f.notifications.take()) != 0 {
		t.Fatal("initial list replayed existing objects as creates")
	}
	source := sourceObject("a")
	source.SetName("created-after-start")
	if _, err := dynamic.Resource(configMapsGVR).Namespace("a").Create(t.Context(), source, metav1.CreateOptions{}); err != nil {
		t.Fatal(err)
	}
	poll("new event", func() bool {
		f.notifications.mu.Lock()
		defer f.notifications.mu.Unlock()
		return f.notifications.targets["local-a"] == 1 && f.notifications.targets["global"] == 1
	})
	if watchStarts.Load() != 1 {
		t.Fatal("identical subscriptions opened separate watches")
	}
	for _, object := range []client.Object{&capsulev1beta2.TenantResource{Name: "a", Namespace: "a"}, &capsulev1beta2.TenantResource{Name: "b", Namespace: "b"}, &capsulev1beta2.GlobalTenantResource{Name: "global"}} {
		if err := f.client.Delete(t.Context(), object); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := f.watcher.Reconcile(t.Context(), reconcile.Request{}); err != nil {
		t.Fatal(err)
	}
	poll("removal", func() bool {
		metric := &dto.Metric{}
		_ = f.watcher.metrics.ActiveWatches.Write(metric)
		return metric.GetGauge().GetValue() == 0
	})
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("watch shutdown did not join goroutines")
	}
	if !f.watcher.NeedLeaderElection() {
		t.Fatal("watchers must run on elected replica")
	}
}

func TestBookkeepingComparisonPreservesUserChanges(t *testing.T) {
	resource := configMapsGVR
	resource.Group = capsulev1beta2.GroupVersion.Group
	resource.Resource = "tenantresources"
	before := sourceObject("a")
	before.SetAnnotations(map[string]string{"user": "keep"})
	after := before.DeepCopy()
	after.SetResourceVersion("2")
	after.SetAnnotations(map[string]string{"user": "changed"})
	if bookkeepingEvent(resource, before, after) {
		t.Fatal("user annotations ignored")
	}
	after.SetAnnotations(before.GetAnnotations())
	after.SetGeneration(before.GetGeneration() + 1)
	if bookkeepingEvent(resource, before, after) {
		t.Fatal("spec update ignored")
	}
	if before.GetAnnotations()["user"] != "keep" {
		t.Fatal("comparison mutated input")
	}
	after = before.DeepCopy()
	now := metav1.Now()
	after.SetDeletionTimestamp(&now)
	if bookkeepingEvent(resource, before, after) {
		t.Fatal("deletion timestamp transition ignored")
	}
	before = after.DeepCopy()
	after.SetFinalizers([]string{"projectcapsule.dev/test"})
	if !bookkeepingEvent(resource, before, after) {
		t.Fatal("finalizer-only update should remain quiet")
	}
	f := watchFixtureFor(t, 0)
	canceled, cancel := context.WithCancel(t.Context())
	cancel()
	f.watcher.enqueueEvent(canceled, resource, admissionv1.Create, nil, after)
	if len(f.watcher.events) != 0 {
		t.Fatal("canceled stream accepted an event")
	}
}

func TestLocalWatchScopesAndGlobalReplacement(t *testing.T) {
	f := watchFixtureFor(t, 0)
	global := &capsulev1beta2.GlobalTenantResource{}
	if err := f.client.Get(t.Context(), client.ObjectKey{Name: "global"}, global); err != nil {
		t.Fatal(err)
	}
	if err := f.client.Delete(t.Context(), global); err != nil {
		t.Fatal(err)
	}
	if _, err := f.watcher.Reconcile(t.Context(), reconcile.Request{}); err != nil {
		t.Fatal(err)
	}
	for _, namespace := range []string{"a", "b"} {
		if _, ok := f.watcher.desired[resourceWatchKey{resource: configMapsGVR, namespace: namespace}]; !ok {
			t.Fatalf("missing local watch: %s", namespace)
		}
	}
	if len(f.watcher.desired) != 2 {
		t.Fatal("local subscriptions opened cluster-wide or duplicate watches")
	}
	ctx, cancel := context.WithCancel(t.Context())
	running := map[resourceWatchKey]runningResourceWatch{}
	f.watcher.syncWatches(ctx, running)
	defer func() {
		cancel()
		for _, watch := range running {
			<-watch.done
		}
	}()
	dynamic := f.watcher.dynamic.(*dynamicfake.FakeDynamicClient)
	if err := wait.PollUntilContextTimeout(t.Context(), time.Millisecond, 5*time.Second, true, func(context.Context) (bool, error) {
		seen := map[string]bool{}
		for _, action := range dynamic.Actions() {
			if action.GetVerb() == "list" || action.GetVerb() == "watch" {
				if action.GetNamespace() != "a" && action.GetNamespace() != "b" {
					t.Fatalf("unscoped informer action: %s", action.GetNamespace())
				}
				seen[action.GetVerb()+action.GetNamespace()] = true
			}
		}
		return len(seen) == 4, nil
	}); err != nil {
		t.Fatal(err)
	}
	global.ResourceVersion = ""
	if err := f.client.Create(t.Context(), global); err != nil {
		t.Fatal(err)
	}
	if _, err := f.watcher.Reconcile(t.Context(), reconcile.Request{}); err != nil {
		t.Fatal(err)
	}
	f.watcher.syncWatches(ctx, running)
	if len(running) != 1 {
		t.Fatal("global subscription did not replace local streams")
	}
	if _, ok := running[resourceWatchKey{resource: configMapsGVR}]; !ok {
		t.Fatal("missing global stream")
	}
	if err := f.client.Delete(t.Context(), global); err != nil {
		t.Fatal(err)
	}
	if _, err := f.watcher.Reconcile(t.Context(), reconcile.Request{}); err != nil {
		t.Fatal(err)
	}
	f.watcher.syncWatches(ctx, running)
	if len(running) != 2 {
		t.Fatal("removing global subscription did not restore local streams")
	}
}
