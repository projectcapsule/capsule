// Copyright 2020-2025 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package cache_test

import (
	"context"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/go-logr/logr"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/projectcapsule/capsule/internal/cache"
	"github.com/projectcapsule/capsule/pkg/api/meta"
)

// makeSA builds meta.NamespacedRFC1123ObjectReferenceWithNamespace without depending
// on the concrete field types (they're string aliases in Capsule).
func makeSA(ns, name string) meta.NamespacedRFC1123ObjectReferenceWithNamespace {
	var sa meta.NamespacedRFC1123ObjectReferenceWithNamespace

	v := reflect.ValueOf(&sa).Elem()

	nsField := v.FieldByName("Namespace")
	if !nsField.IsValid() || nsField.Kind() != reflect.String || !nsField.CanSet() {
		panic("meta.NamespacedRFC1123ObjectReferenceWithNamespace.Namespace is not a settable string-kind field")
	}
	nsField.SetString(ns)

	nameField := v.FieldByName("Name")
	if !nameField.IsValid() || nameField.Kind() != reflect.String || !nameField.CanSet() {
		panic("meta.NamespacedRFC1123ObjectReferenceWithNamespace.Name is not a settable string-kind field")
	}
	nameField.SetString(name)

	return sa
}

func TestImpersonationCache_Basics(t *testing.T) {
	t.Parallel()

	c := cache.NewImpersonationCache()

	t.Run("Get on empty cache returns false", func(t *testing.T) {
		t.Parallel()
		_, ok := c.Get("ns", "sa")
		if ok {
			t.Fatalf("expected ok=false on empty cache")
		}
	})

	t.Run("Set(nil) does not count as present", func(t *testing.T) {
		t.Parallel()
		c.Set("ns", "sa", nil)
		if entries := c.Stats(); entries != 1 {
			t.Fatalf("expected Stats()=1 after Set(nil), got %d", entries)
		}
		_, ok := c.Get("ns", "sa")
		if ok {
			t.Fatalf("expected ok=false because stored client is nil")
		}
	})

	t.Run("Invalidate removes entry", func(t *testing.T) {
		t.Parallel()
		c.Set("ns", "sa", nil)
		c.Invalidate("ns", "sa")
		if entries := c.Stats(); entries != 0 {
			t.Fatalf("expected Stats()=0 after Invalidate, got %d", entries)
		}
	})

	t.Run("Clear drops all entries", func(t *testing.T) {
		t.Parallel()
		c.Set("a", "x", nil)
		c.Set("b", "y", nil)

		if entries := c.Stats(); entries != 2 {
			t.Fatalf("expected Stats()=2 before Clear, got %d", entries)
		}
		c.Clear()
		if entries := c.Stats(); entries != 0 {
			t.Fatalf("expected Stats()=0 after Clear, got %d", entries)
		}
	})
}

func TestImpersonationCache_LoadOrCreate_ErrorDoesNotCache(t *testing.T) {
	t.Parallel()

	cache := cache.NewImpersonationCache()
	ctx := context.Background()
	log := logr.Discard()
	sa := makeSA("monitoring", "alertmanager-sa")

	// Intentionally invalid REST config -> client creation should fail quickly.
	invalidREST := &rest.Config{
		Host: "", // empty host should cause client construction to fail
	}

	// Use the real Kubernetes scheme (no apiserver needed to create a controller-runtime client object).
	sch := scheme.Scheme

	cl, err := cache.LoadOrCreate(ctx, log, invalidREST, sch, sa)
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	if cl != nil {
		t.Fatalf("expected client=nil on error, got %#v", cl)
	}
	if entries := cache.Stats(); entries != 0 {
		t.Fatalf("expected cache to remain empty on error, got %d entries", entries)
	}

	if _, ok := cache.Get("monitoring", "alertmanager-sa"); ok {
		t.Fatalf("expected cache Get to be false after failed LoadOrCreate")
	}
}

func TestImpersonationCache_LoadOrCreate_SuccessCachesAndReturnsSameInstance(t *testing.T) {
	t.Parallel()

	cache := cache.NewImpersonationCache()
	ctx := context.Background()
	log := logr.Discard()
	sa := makeSA("monitoring", "alertmanager-sa")

	// Provide a REST config that is syntactically valid so controller-runtime client can be constructed
	// without needing a live apiserver.
	validREST := &rest.Config{
		Host: "https://127.0.0.1", // no connectivity required for client object creation
		TLSClientConfig: rest.TLSClientConfig{
			Insecure: true,
		},
	}

	sch := scheme.Scheme

	cl1, err := cache.LoadOrCreate(ctx, log, validREST, sch, sa)
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if cl1 == nil {
		t.Fatalf("expected non-nil client")
	}

	// Should be cached now.
	if entries := cache.Stats(); entries != 1 {
		t.Fatalf("expected 1 cache entry, got %d", entries)
	}

	cl2, err := cache.LoadOrCreate(ctx, log, validREST, sch, sa)
	if err != nil {
		t.Fatalf("expected nil error on second LoadOrCreate, got %v", err)
	}
	if cl2 == nil {
		t.Fatalf("expected non-nil client on second LoadOrCreate")
	}

	// Must return the cached instance.
	if cl1 != cl2 {
		t.Fatalf("expected same client instance from cache; got different pointers")
	}

	// Get should return it and ok=true.
	got, ok := cache.Get("monitoring", "alertmanager-sa")
	if !ok {
		t.Fatalf("expected ok=true from Get after LoadOrCreate")
	}
	if got != cl1 {
		t.Fatalf("expected Get to return cached client instance")
	}
}

func TestImpersonationCache_LoadOrCreate_ConcurrentOnlyCachesOne(t *testing.T) {
	t.Parallel()

	cache := cache.NewImpersonationCache()
	ctx := context.Background()
	log := logr.Discard()
	sa := makeSA("monitoring", "alertmanager-sa")

	validREST := &rest.Config{
		Host: "https://127.0.0.1",
		TLSClientConfig: rest.TLSClientConfig{
			Insecure: true,
		},
	}
	sch := scheme.Scheme

	const goroutines = 25
	start := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(goroutines)

	results := make([]client.Client, goroutines)
	errs := make([]error, goroutines)

	for i := 0; i < goroutines; i++ {
		i := i
		go func() {
			defer wg.Done()
			<-start
			cl, err := cache.LoadOrCreate(ctx, log, validREST, sch, sa)
			results[i] = cl
			errs[i] = err
		}()
	}

	close(start)

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatalf("timeout waiting for goroutines")
	}

	// All should succeed and return a non-nil client.
	var first client.Client
	for i := 0; i < goroutines; i++ {
		if errs[i] != nil {
			t.Fatalf("expected nil error for goroutine %d, got %v", i, errs[i])
		}
		if results[i] == nil {
			t.Fatalf("expected non-nil client for goroutine %d", i)
		}
		if i == 0 {
			first = results[i]
			continue
		}
		// Even with races (double-check store), function returns the stored instance.
		if results[i] != first {
			t.Fatalf("expected all goroutines to get same cached client instance; got mismatch at %d", i)
		}
	}

	// Cache should have exactly one entry.
	if entries := cache.Stats(); entries != 1 {
		t.Fatalf("expected exactly 1 cache entry after concurrent LoadOrCreate, got %d", entries)
	}
}

func TestImpersonationCache_LoadOrCreate_DifferentKeysCreateDifferentEntries(t *testing.T) {
	t.Parallel()

	cache := cache.NewImpersonationCache()
	ctx := context.Background()
	log := logr.Discard()

	validREST := &rest.Config{
		Host: "https://127.0.0.1",
		TLSClientConfig: rest.TLSClientConfig{
			Insecure: true,
		},
	}
	sch := scheme.Scheme

	sa1 := makeSA("monitoring", "sa-one")
	sa2 := makeSA("monitoring", "sa-two")

	cl1, err := cache.LoadOrCreate(ctx, log, validREST, sch, sa1)
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	cl2, err := cache.LoadOrCreate(ctx, log, validREST, sch, sa2)
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}

	if cl1 == nil || cl2 == nil {
		t.Fatalf("expected both clients to be non-nil")
	}
	if cl1 == cl2 {
		t.Fatalf("expected different clients for different keys")
	}
	if entries := cache.Stats(); entries != 2 {
		t.Fatalf("expected 2 cache entries, got %d", entries)
	}
}

func TestImpersonationCache_InvalidateThenLoadOrCreateCreatesNewInstance(t *testing.T) {
	t.Parallel()

	cache := cache.NewImpersonationCache()
	ctx := context.Background()
	log := logr.Discard()
	sa := makeSA("monitoring", "alertmanager-sa")

	validREST := &rest.Config{
		Host: "https://127.0.0.1",
		TLSClientConfig: rest.TLSClientConfig{
			Insecure: true,
		},
	}
	sch := scheme.Scheme

	cl1, err := cache.LoadOrCreate(ctx, log, validREST, sch, sa)
	if err != nil || cl1 == nil {
		t.Fatalf("expected first LoadOrCreate success, err=%v cl=%v", err, cl1)
	}

	cache.Invalidate("monitoring", "alertmanager-sa")
	if entries := cache.Stats(); entries != 0 {
		t.Fatalf("expected 0 entries after Invalidate, got %d", entries)
	}

	cl2, err := cache.LoadOrCreate(ctx, log, validREST, sch, sa)
	if err != nil || cl2 == nil {
		t.Fatalf("expected second LoadOrCreate success, err=%v cl=%v", err, cl2)
	}

	// After invalidation, a new client instance should be created and cached.
	if cl1 == cl2 {
		t.Fatalf("expected new client instance after Invalidate, got same pointer")
	}
	if entries := cache.Stats(); entries != 1 {
		t.Fatalf("expected 1 entry after re-LoadOrCreate, got %d", entries)
	}
}
