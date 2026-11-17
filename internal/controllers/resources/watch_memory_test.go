// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package resources

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/dynamic"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	"k8s.io/client-go/rest"
	clientcache "k8s.io/client-go/tools/cache"

	"github.com/projectcapsule/capsule/internal/cache"
)

func TestResourceWatchRetainsBoundedObjectSnapshots(t *testing.T) {
	f := watchFixtureFor(t, 0)
	f.watcher.baseREST = nil
	dyn := f.watcher.dynamic.(*dynamicfake.FakeDynamicClient)
	for i := range 2000 {
		object := &corev1.ConfigMap{Name: fmt.Sprintf("input-%04d", i), Namespace: "a", ResourceVersion: "1", Data: map[string]string{"value": strings.Repeat("x", 16*1024)}}
		if err := dyn.Tracker().Add(object); err != nil {
			t.Fatal(err)
		}
	}
	source, err := f.watcher.resourceStream(t.Context(), resourceWatchKey{resource: configMapsGVR, namespace: "a"}, schema.GroupVersionKind{Version: "v1", Kind: "ConfigMap"})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() { defer close(done); source.RunWithContext(ctx) }()
	defer func() { cancel(); <-done }()
	if err := wait.PollUntilContextTimeout(t.Context(), time.Millisecond, 5*time.Second, true, func(context.Context) (bool, error) { return source.HasSynced(), nil }); err != nil {
		t.Fatal(err)
	}
	if count := len(source.GetStore().List()); count > 1024 {
		t.Fatalf("watch retained %d full objects; snapshots must have a fixed count and byte budget", count)
	}
	if source.GetStore().Bytes() > cache.MaxWatchSnapshotBytes {
		t.Fatal("watch snapshot byte budget exceeded")
	}
	cancel()
	<-done
	if source.GetStore().Bytes() != 0 || len(source.GetStore().List()) != 0 {
		t.Fatal("stopped watch retained snapshots")
	}
}

func TestWatchStreamColdUpdatesAndRelistSemantics(t *testing.T) {
	f := watchFixtureFor(t, 0)
	s := &resourceWatchStream{owner: f.watcher, store: cache.NewReplicationWatchSnapshots(), slots: f.watcher.listSlots}
	var creates, updates, deletes int
	var previous string
	s.AddEventHandler(clientcache.ResourceEventHandlerFuncs{
		AddFunc:    func(any) { creates++ },
		UpdateFunc: func(old, next any) { updates++; previous = old.(*unstructured.Unstructured).GetResourceVersion() },
		DeleteFunc: func(any) { deletes++ },
	})
	store := &resourceWatchStore{stream: s, done: t.Context().Done()}
	object := sourceObject("a")
	object.SetResourceVersion("1")
	s.observe(object, false)
	if err := store.Replace(nil, "1"); err != nil {
		t.Fatal(err)
	}
	if creates+updates+deletes != 0 {
		t.Fatal("initial baseline emitted events")
	}
	changed := object.DeepCopy()
	changed.SetResourceVersion("2")
	if err := store.Update(changed); err != nil {
		t.Fatal(err)
	}
	if updates != 1 || previous != "1" {
		t.Fatal("warm update lost immutable old state")
	}
	s.store.Delete(object.GetNamespace() + "/" + object.GetName())
	changed = changed.DeepCopy()
	changed.SetResourceVersion("3")
	if err := store.Update(changed); err != nil {
		t.Fatal(err)
	}
	if updates != 1 || creates != 0 {
		t.Fatal("eviction fabricated a create or a null oldObject update")
	}
	changed = changed.DeepCopy()
	changed.SetResourceVersion("4")
	if err := store.Update(changed); err != nil {
		t.Fatal(err)
	}
	if updates != 2 || previous != "3" {
		t.Fatal("cold observation did not restore subsequent old/new matching")
	}
	s.stamp++
	s.complete = false
	store.Replace(nil, "5")
	if deletes != 0 {
		t.Fatal("partial baseline inferred a deletion")
	}
	s.complete = true
	store.Replace(nil, "5")
	if deletes != 1 {
		t.Fatal("completed relist did not notify a retained deletion")
	}
}

func TestWatchListResponseBoundsAndAdaptivePagination(t *testing.T) {
	var largestLimit, requests atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Query().Get("watch") == "true" {
			w.Header().Set("Content-Type", "application/json")
			w.(http.Flusher).Flush()
			<-r.Context().Done()
			return
		}
		limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
		if limit == 0 {
			t.Error("unbounded LIST request")
			http.Error(w, "limit required", 400)
			return
		}
		requests.Add(1)
		for old := largestLimit.Load(); int64(limit) > old && !largestLimit.CompareAndSwap(old, int64(limit)); old = largestLimit.Load() {
		}
		start, _ := strconv.Atoi(r.URL.Query().Get("continue"))
		end := min(start+limit, 32)
		items := make([]map[string]any, 0, end-start)
		for i := start; i < end; i++ {
			items = append(items, map[string]any{"apiVersion": "v1", "kind": "ConfigMap", "metadata": map[string]any{"name": fmt.Sprintf("source-%d", i), "namespace": "a", "resourceVersion": "42"}, "data": map[string]any{"value": strings.Repeat("x", 192*1024)}})
		}
		next := ""
		if end < 32 {
			next = strconv.Itoa(end)
		}
		json.NewEncoder(w).Encode(map[string]any{"apiVersion": "v1", "kind": "ConfigMapList", "metadata": map[string]any{"resourceVersion": "42", "continue": next}, "items": items})
	}))
	defer server.Close()
	f := watchFixtureFor(t, 0)
	f.watcher.baseREST = &rest.Config{Host: server.URL}
	config := rest.CopyConfig(f.watcher.baseREST)
	limitWatchListResponses(config)
	dyn, err := dynamic.NewForConfig(config)
	if err != nil {
		t.Fatal(err)
	}
	f.watcher.dynamic = dyn
	source, err := f.watcher.resourceStream(t.Context(), resourceWatchKey{resource: configMapsGVR, namespace: "a"}, corev1.SchemeGroupVersion.WithKind("ConfigMap"))
	if err != nil {
		t.Fatal(err)
	}
	if err := runWatchUntilReadyOrError(t, source, func() {
		if source.store.Bytes() > cache.MaxWatchSnapshotBytes {
			t.Error("retention budget exceeded")
		}
	}); err != nil {
		t.Fatal(err)
	}
	if largestLimit.Load() != watchListPageSize || requests.Load() < 2 {
		t.Fatalf("adaptive pagination not exercised: max=%d requests=%d", largestLimit.Load(), requests.Load())
	}
}

func TestWatchEventByteBudgetAndCancellationRelease(t *testing.T) {
	f := watchFixtureFor(t, 0)
	object := sourceObject("a")
	object.Object["data"] = map[string]any{"value": strings.Repeat("x", 128*1024)}
	for range 256 {
		f.watcher.enqueueEvent(t.Context(), configMapsGVR, resourceWatchIdentity{}, "CREATE", nil, object)
	}
	if f.watcher.eventBytes.Load() > maxWatchEventBytes || len(f.watcher.events) == 0 || len(f.watcher.events) >= cap(f.watcher.events) {
		t.Fatal("queue was bounded only by object count")
	}
	for len(f.watcher.events) > 0 {
		event := <-f.watcher.events
		f.watcher.releaseEvent(event)
	}
	if f.watcher.eventBytes.Load() != 0 {
		t.Fatal("dequeued bytes were not released")
	}
	ctx, cancel := context.WithCancel(t.Context())
	f.watcher.enqueueEvent(ctx, configMapsGVR, resourceWatchIdentity{}, "CREATE", nil, object)
	cancel()
	f.watcher.processEvent(t.Context(), <-f.watcher.events)
	if f.watcher.eventBytes.Load() != 0 || f.lists != 0 || f.gets != 0 {
		t.Fatal("canceled stream retained queued bytes or performed subscription lookups")
	}
	var exceeded atomic.Bool
	body := &watchListBody{ReadCloser: io.NopCloser(strings.NewReader(strings.Repeat("x", 1025))), remaining: 1024, exceeded: &exceeded}
	if _, err := io.ReadAll(body); err != errWatchListResponseLimit || !exceeded.Load() {
		t.Fatalf("response limit: %v", err)
	}
}

func TestWatchBaselinePagesPreserveSnapshotAndContext(t *testing.T) {
	f := watchFixtureFor(t, 0)
	s := &resourceWatchStream{owner: f.watcher, store: cache.NewReplicationWatchSnapshots(), slots: f.watcher.listSlots}
	calls := 0
	s.page = func(ctx context.Context, options metav1.ListOptions) (*unstructured.UnstructuredList, error) {
		calls++
		if _, bounded := ctx.Deadline(); !bounded {
			t.Fatal("shared LIST slot has no request deadline")
		}
		if ctx.Err() != nil || options.Limit != watchListPageSize || options.ResourceVersion != "" {
			t.Fatal("pagination lost cancellation or requested an unbounded/stale page")
		}
		start := 0
		if options.Continue != "" {
			start = 32
		}
		page := &unstructured.UnstructuredList{}
		page.SetResourceVersion("42")
		for i := start; i < min(start+32, 48); i++ {
			object := sourceObject("a")
			object.SetName(fmt.Sprint(i))
			page.Items = append(page.Items, *object)
		}
		if start == 0 {
			page.SetContinue("next")
		}
		return page, nil
	}
	if _, err := s.listPages(t.Context(), metav1.ListOptions{ResourceVersion: "0"}); err != nil {
		t.Fatal(err)
	}
	if calls != 2 || !s.complete || len(s.store.List()) != 48 {
		t.Fatalf("baseline pagination: calls=%d complete=%t objects=%d", calls, s.complete, len(s.store.List()))
	}
}

func TestBlockedWatchPageCancellationReleasesSharedSlot(t *testing.T) {
	f := watchFixtureFor(t, 0)
	s := &resourceWatchStream{owner: f.watcher, store: cache.NewReplicationWatchSnapshots(), slots: f.watcher.listSlots}
	started := make(chan struct{})
	s.page = func(ctx context.Context, _ metav1.ListOptions) (*unstructured.UnstructuredList, error) {
		close(started)
		<-ctx.Done()
		return nil, ctx.Err()
	}
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { _, err := s.listPages(ctx, metav1.ListOptions{}); done <- err }()
	<-started
	cancel()
	if err := <-done; err != context.Canceled || len(s.slots) != 0 {
		t.Fatalf("cancellation lost or slot retained: %v", err)
	}
}

func TestWatchListDecodingConcurrencyAndWaitingCancellation(t *testing.T) {
	f := watchFixtureFor(t, 0)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	started := make(chan struct{}, 3)
	done := make(chan error, 3)
	for range 3 {
		s := &resourceWatchStream{owner: f.watcher, store: cache.NewReplicationWatchSnapshots(), slots: f.watcher.listSlots}
		s.page = func(ctx context.Context, _ metav1.ListOptions) (*unstructured.UnstructuredList, error) {
			started <- struct{}{}
			<-ctx.Done()
			return nil, ctx.Err()
		}
		go func() { _, err := s.listPages(ctx, metav1.ListOptions{}); done <- err }()
	}
	<-started
	<-started
	cancel()
	for range 3 {
		if err := <-done; err != context.Canceled {
			t.Fatalf("canceled page: %v", err)
		}
	}
	if len(started) != 0 || len(f.watcher.listSlots) != 0 {
		t.Fatal("a waiting page decoded despite cancellation or retained its slot")
	}
}
