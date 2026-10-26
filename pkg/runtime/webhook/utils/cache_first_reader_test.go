// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package utils

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
)

func newScheme(t *testing.T) *runtime.Scheme {
	t.Helper()

	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}

	if err := capsulev1beta2.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}

	return scheme
}

// countingAPIReader records how many Get calls reach the API server.
func countingAPIReader(t *testing.T, gets *int, objs ...client.Object) client.Reader {
	t.Helper()

	return fake.NewClientBuilder().WithScheme(newScheme(t)).WithObjects(objs...).WithInterceptorFuncs(interceptor.Funcs{
		Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
			*gets++

			return c.Get(ctx, key, obj, opts...)
		},
	}).Build()
}

func TestCacheFirstReaderServesTenantLookupsFromCache(t *testing.T) {
	t.Parallel()

	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "tenant-a-dev"}}
	tnt := &capsulev1beta2.Tenant{ObjectMeta: metav1.ObjectMeta{Name: "tenant-a"}}
	cache := fake.NewClientBuilder().WithScheme(newScheme(t)).WithObjects(ns, tnt).Build()

	apiGets := 0
	reader := NewCacheFirstReader(cache, countingAPIReader(t, &apiGets, ns, tnt))

	if err := reader.Get(context.Background(), client.ObjectKeyFromObject(ns), &corev1.Namespace{}); err != nil {
		t.Fatalf("namespace: %v", err)
	}

	if err := reader.Get(context.Background(), client.ObjectKeyFromObject(tnt), &capsulev1beta2.Tenant{}); err != nil {
		t.Fatalf("tenant: %v", err)
	}

	if apiGets != 0 {
		t.Fatalf("expected the cache to answer, got %d direct reads", apiGets)
	}
}

// A Namespace created a few milliseconds ago may not be in the cache yet: the
// reader must find it on the API server instead of reporting it missing.
func TestCacheFirstReaderFallsBackForObjectsMissingFromCache(t *testing.T) {
	t.Parallel()

	fresh := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "tenant-a-new"}}
	cache := fake.NewClientBuilder().WithScheme(newScheme(t)).Build()

	apiGets := 0
	reader := NewCacheFirstReader(cache, countingAPIReader(t, &apiGets, fresh))

	got := &corev1.Namespace{}
	if err := reader.Get(context.Background(), client.ObjectKeyFromObject(fresh), got); err != nil {
		t.Fatalf("expected the API server to answer, got %v", err)
	}

	if got.Name != fresh.Name || apiGets != 1 {
		t.Fatalf("expected one direct read returning %q, got %d reads and %q", fresh.Name, apiGets, got.Name)
	}
}

// Other types keep the direct read, the cache must not start informers for them.
func TestCacheFirstReaderKeepsDirectReadsForOtherTypes(t *testing.T) {
	t.Parallel()

	cm := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "cm", Namespace: "tenant-a-dev"}}
	cacheGets := 0
	cache := countingAPIReader(t, &cacheGets, cm)

	apiGets := 0
	reader := NewCacheFirstReader(cache, countingAPIReader(t, &apiGets, cm))

	if err := reader.Get(context.Background(), client.ObjectKeyFromObject(cm), &corev1.ConfigMap{}); err != nil {
		t.Fatal(err)
	}

	if cacheGets != 0 || apiGets != 1 {
		t.Fatalf("expected one direct read and no cache read, got api=%d cache=%d", apiGets, cacheGets)
	}
}
