// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package invalidator

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/internal/cache"
	"github.com/projectcapsule/capsule/pkg/runtime/configuration"
	"github.com/projectcapsule/capsule/pkg/runtime/indexers/tenantresource"
)

func TestReplicationCacheInvalidationExcludesAdmissionState(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))
	require.NoError(t, capsulev1beta2.AddToScheme(scheme))
	lists := []string{}
	wantErr := errors.New("replication cache unavailable")
	fail := false
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(&capsulev1beta2.CapsuleConfiguration{Name: "default"}).WithInterceptorFuncs(interceptor.Funcs{
		List: func(ctx context.Context, c client.WithWatch, list client.ObjectList, opts ...client.ListOption) error {
			switch list.(type) {
			case *capsulev1beta2.TenantResourceList, *capsulev1beta2.GlobalTenantResourceList:
			default:
				t.Fatalf("background worker read admission state %T", list)
			}
			lists = append(lists, fmt.Sprintf("%T", list))
			if fail {
				return wantErr
			}
			return c.List(ctx, list, opts...)
		},
	}).Build()
	r := &CacheInvalidator{Client: c, ReplicationsOnly: true, ImpersonationCache: cache.NewImpersonationCache(), Configuration: configuration.NewCapsuleConfiguration(t.Context(), c, c, &rest.Config{Host: "https://unused.example.com"}, "default")}
	require.NoError(t, r.rebuildCaches(t.Context(), logr.Discard()))
	require.Len(t, lists, 2)
	require.Equal(t, "capsule/replications/caches", r.controllerName())
	fail = true
	require.ErrorIs(t, r.rebuildCaches(t.Context(), logr.Discard()), wantErr)
	// Core-only processes do not read replication objects on configuration changes.
	r.ImpersonationCache = nil
	before := len(lists)
	require.NoError(t, r.rebuildImpersonationCache(t.Context(), logr.Discard()))
	require.Len(t, lists, before)
}

func BenchmarkControllerReplicationCacheInvalidation(b *testing.B) {
	for _, count := range []int{1, 1024} {
		b.Run(fmt.Sprintf("unrelated-tenants=%d", count), func(b *testing.B) {
			scheme := runtime.NewScheme()
			if err := capsulev1beta2.AddToScheme(scheme); err != nil {
				b.Fatal(err)
			}
			objects := []client.Object{&capsulev1beta2.CapsuleConfiguration{Name: "default"}}
			for i := range count {
				objects = append(objects, &capsulev1beta2.Tenant{Name: fmt.Sprintf("tenant-%d", i)}, &capsulev1beta2.RuleStatus{Name: "profile", Namespace: fmt.Sprintf("ns-%d", i)})
			}
			lists := 0
			c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objects...).WithInterceptorFuncs(interceptor.Funcs{List: func(ctx context.Context, c client.WithWatch, list client.ObjectList, opts ...client.ListOption) error {
				lists++
				return c.List(ctx, list, opts...)
			}}).Build()
			r := &CacheInvalidator{Client: c, ReplicationsOnly: true, ImpersonationCache: cache.NewImpersonationCache(), AdditionalImpersonationCaches: []*cache.ImpersonationCache{cache.NewImpersonationCache()}, Configuration: configuration.NewCapsuleConfiguration(b.Context(), c, c, &rest.Config{Host: "https://unused.example.com"}, "default")}
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				if err := r.rebuildCaches(b.Context(), logr.Discard()); err != nil {
					b.Fatal(err)
				}
			}
			b.ReportMetric(float64(lists)/float64(b.N), "LIST/op")
		})
	}
}

func TestReplicationInvalidationRetiresAdditionalClientBudgets(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, capsulev1beta2.AddToScheme(scheme))
	for _, primary := range []bool{false, true} {
		t.Run(fmt.Sprint(primary), func(t *testing.T) {
			c := fake.NewClientBuilder().WithScheme(scheme).
				WithIndex(&capsulev1beta2.GlobalTenantResource{}, tenantresource.ServiceAccountIndexerFieldName, func(client.Object) []string { return nil }).
				WithIndex(&capsulev1beta2.TenantResource{}, tenantresource.ServiceAccountIndexerFieldName, func(client.Object) []string { return nil }).Build()
			bootstrap := cache.NewImpersonationCache()
			r := &CacheInvalidator{Client: c, AdditionalImpersonationCaches: []*cache.ImpersonationCache{bootstrap, nil}, Configuration: configuration.NewCapsuleConfiguration(t.Context(), c, c, &rest.Config{Host: "https://unused.example.com"}, "default")}
			if primary {
				r.ImpersonationCache = cache.NewImpersonationCache()
				bootstrap.Set("tenant-a", "old", c)
				bootstrap.Set("tenant-b", "active", c)
				require.NoError(t, r.invalidateServiceAccount(t.Context(), &corev1.ServiceAccount{Name: "old", Namespace: "tenant-a"}))
				_, exists := bootstrap.Get("tenant-a", "old")
				require.False(t, exists)
				_, exists = bootstrap.Get("tenant-b", "active")
				require.True(t, exists, "retirement must remain namespace scoped")
			}
			bootstrap.Set("tenant-a", "old", c)
			require.NoError(t, r.rebuildImpersonationCache(t.Context(), logr.Discard()))
			_, exists := bootstrap.Get("tenant-a", "old")
			require.False(t, exists, "periodic and configuration invalidation must retire bootstrap clients")
		})
	}
}
