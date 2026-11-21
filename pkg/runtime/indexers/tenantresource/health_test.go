// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package tenantresource

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	toolscache "k8s.io/client-go/tools/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api"
)

func TestGlobalNamespaceProfileIndexLifecycle(t *testing.T) {
	index := GlobalHealthNamespaces{}
	scheme := runtime.NewScheme()
	require.NoError(t, capsulev1beta2.AddToScheme(scheme))
	source := &capsulev1beta2.GlobalTenantResource{Name: "profile", Spec: capsulev1beta2.GlobalTenantResourceSpec{Scope: api.ResourceScopeNamespace}}
	source.Spec.Resources = []capsulev1beta2.ResourceSpec{{}, {NamespaceSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"profile": "selected"}}}, {NamespaceSelector: &metav1.LabelSelector{}}}
	require.Equal(t, []string{GlobalNamespaceProfileIndexValue}, index.Func()(source), "multiple blocks must not duplicate the index key")
	source.Spec.HealthChecks = &capsulev1beta2.TenantResourceHealthChecks{Wait: true}
	require.Equal(t, []string{"true", GlobalNamespaceProfileIndexValue}, index.Func()(source))
	source.Spec.Scope = api.ResourceScopeNone
	require.Nil(t, index.Func()(source))
	source.Spec.Scope = api.ResourceScopeNamespace
	source.Spec.HealthChecks = nil
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(source).WithIndex(index.Object(), index.Field(), index.Func()).Build()
	list := &capsulev1beta2.GlobalTenantResourceList{}
	lookup := func(want int) {
		t.Helper()
		require.NoError(t, c.List(t.Context(), list, client.MatchingFields{index.Field(): GlobalNamespaceProfileIndexValue}))
		require.Len(t, list.Items, want)
	}
	lookup(1)
	require.NoError(t, c.Get(t.Context(), client.ObjectKeyFromObject(source), source))
	source.Spec.Resources = nil
	require.NoError(t, c.Update(t.Context(), source))
	lookup(0)
	source.Spec.Resources = []capsulev1beta2.ResourceSpec{{NamespaceSelector: &metav1.LabelSelector{}}}
	require.NoError(t, c.Update(t.Context(), source))
	lookup(1)
	require.NoError(t, c.Delete(t.Context(), source))
	lookup(0)
}

func BenchmarkNamespaceProfileIndex(b *testing.B) {
	for _, count := range []int{1, 1024, 16384} {
		b.Run(fmt.Sprint(count), func(b *testing.B) {
			index := GlobalHealthNamespaces{}
			store := toolscache.NewIndexer(toolscache.MetaNamespaceKeyFunc, toolscache.Indexers{index.Field(): func(obj any) ([]string, error) { return index.Func()(obj.(client.Object)), nil }})
			source := &capsulev1beta2.GlobalTenantResource{Name: "selected", Spec: capsulev1beta2.GlobalTenantResourceSpec{Scope: api.ResourceScopeNamespace}}
			source.Spec.Resources = []capsulev1beta2.ResourceSpec{{NamespaceSelector: &metav1.LabelSelector{}}}
			require.NoError(b, store.Add(source))
			for i := range count {
				require.NoError(b, store.Add(&capsulev1beta2.GlobalTenantResource{Name: fmt.Sprintf("unrelated-%d", i)}))
			}
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				items, err := store.ByIndex(index.Field(), GlobalNamespaceProfileIndexValue)
				if err != nil || len(items) != 1 {
					b.Fatalf("lookup failed: %v", err)
				}
			}
		})
	}
}

func TestGlobalHealthNamespacesIndex(t *testing.T) {
	index := GlobalHealthNamespaces{}
	scheme := runtime.NewScheme()
	require.NoError(t, capsulev1beta2.AddToScheme(scheme))
	parent := &capsulev1beta2.GlobalTenantResource{Name: "selected", Spec: capsulev1beta2.GlobalTenantResourceSpec{Scope: api.ResourceScopeNamespace, TenantResourceCommonSpec: capsulev1beta2.TenantResourceCommonSpec{HealthChecks: &capsulev1beta2.TenantResourceHealthChecks{Wait: true}}}}
	require.Equal(t, []string{"true"}, index.Func()(parent))
	require.Nil(t, index.Func()(&capsulev1beta2.TenantResource{}))
	unrelated := parent.DeepCopy()
	unrelated.Name = "legacy"
	unrelated.Spec.HealthChecks.Wait = false
	omitted := parent.DeepCopy()
	omitted.Name = "omitted"
	omitted.Spec.HealthChecks = nil
	empty := parent.DeepCopy()
	empty.Name = "empty"
	empty.Spec.HealthChecks = &capsulev1beta2.TenantResourceHealthChecks{}
	otherScope := parent.DeepCopy()
	otherScope.Name = "cluster"
	otherScope.Spec.Scope = api.ResourceScopeNone
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(parent, unrelated, omitted, empty, otherScope).WithIndex(index.Object(), index.Field(), index.Func()).Build()
	list := &capsulev1beta2.GlobalTenantResourceList{}
	require.NoError(t, c.List(t.Context(), list, client.MatchingFields{index.Field(): "true"}))
	require.Len(t, list.Items, 1)
	require.Equal(t, parent.Name, list.Items[0].Name)
	require.NoError(t, c.Get(t.Context(), client.ObjectKeyFromObject(parent), parent))
	parent.Spec.HealthChecks.Wait = false
	require.NoError(t, c.Update(t.Context(), parent))
	require.NoError(t, c.List(t.Context(), list, client.MatchingFields{index.Field(): "true"}))
	require.Empty(t, list.Items)
	require.NoError(t, c.Delete(t.Context(), parent))
	require.NoError(t, c.List(t.Context(), list, client.MatchingFields{index.Field(): "true"}))
	require.Empty(t, list.Items)
}

func BenchmarkHealthNamespaceIndex(b *testing.B) {
	for _, count := range []int{1, 1024} {
		b.Run(fmt.Sprint(count), func(b *testing.B) {
			index := GlobalHealthNamespaces{}
			scheme := runtime.NewScheme()
			_ = capsulev1beta2.AddToScheme(scheme)
			objects := []client.Object{&capsulev1beta2.GlobalTenantResource{Name: "selected", Spec: capsulev1beta2.GlobalTenantResourceSpec{Scope: api.ResourceScopeNamespace, TenantResourceCommonSpec: capsulev1beta2.TenantResourceCommonSpec{HealthChecks: &capsulev1beta2.TenantResourceHealthChecks{Wait: true}}}}}
			for i := range count {
				objects = append(objects, &capsulev1beta2.GlobalTenantResource{Name: fmt.Sprintf("unrelated-%d", i)})
			}
			store := toolscache.NewIndexer(toolscache.MetaNamespaceKeyFunc, toolscache.Indexers{index.Field(): func(obj any) ([]string, error) { return index.Func()(obj.(client.Object)), nil }})
			for _, object := range objects {
				if err := store.Add(object); err != nil {
					b.Fatal(err)
				}
			}
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				items, err := store.ByIndex(index.Field(), "true")
				if err != nil || len(items) != 1 {
					b.Fatalf("lookup failed: %v", err)
				}
			}
		})
	}
}
