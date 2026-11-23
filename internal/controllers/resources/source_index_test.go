// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package resources

import (
	"fmt"
	"testing"

	"github.com/go-logr/logr"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/sets"
	toolscache "k8s.io/client-go/tools/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/meta"
)

func testNamespaceSourceIndex(t testing.TB, sources ...client.Object) *namespaceSourceIndex {
	t.Helper()
	index := toolscache.NewIndexer(toolscache.MetaNamespaceKeyFunc, toolscache.Indexers{toolscache.NamespaceIndex: toolscache.MetaNamespaceIndexFunc})
	for _, source := range sources {
		require.NoError(t, index.Add(source))
	}
	return &namespaceSourceIndex{index: index}
}

func TestNamespaceSourceLookupRejectsStaleTenantRelationships(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))
	require.NoError(t, capsulev1beta2.AddToScheme(scheme))
	a := &capsulev1beta2.Tenant{Name: "a", UID: "a-uid"}
	b := &capsulev1beta2.Tenant{Name: "b", UID: "b-uid"}
	a.Status.Namespaces = []string{"b-source"}
	namespace := &corev1.Namespace{Name: "b-source", Labels: map[string]string{meta.TenantLabel: b.Name}, OwnerReferences: []metav1.OwnerReference{{APIVersion: capsulev1beta2.GroupVersion.String(), Kind: "Tenant", Name: b.Name, UID: b.UID}}}
	source := &capsulev1beta2.TenantResource{Name: "private", Namespace: namespace.Name, Finalizers: []string{meta.ControllerFinalizer}}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(a, b, namespace, source).Build()
	r := &NamespaceTrigger{client: c, reader: c, sources: testNamespaceSourceIndex(t, source)}
	require.NoError(t, r.replicateNamespacedResources(t.Context(), logr.Discard(), *a, &corev1.Namespace{Name: "a-new"}), "a stale namespace list must not load or execute tenant B's source")
	owned, err := r.sourceNamespaceOwned(t.Context(), namespace.Name, *b)
	require.NoError(t, err)
	require.True(t, owned)
	stale := b.DeepCopy()
	stale.UID = "old-uid"
	owned, err = r.sourceNamespaceOwned(t.Context(), namespace.Name, *stale)
	require.NoError(t, err)
	require.False(t, owned)
}

func TestNamespaceSourceIndexLifecycleAndIsolation(t *testing.T) {
	a := &capsulev1beta2.TenantResource{Namespace: "a-0", Name: "a", UID: "a", Labels: map[string]string{"profile": "one"}}
	b := &capsulev1beta2.TenantResource{Namespace: "b-0", Name: "b", UID: "b"}
	index := testNamespaceSourceIndex(t, a, b)
	got, err := index.list(t.Context(), nil, sets.New("a-0", "a-1"))
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.Equal(t, "a", got[0].Name)
	got[0].Labels["profile"] = "mutated"
	require.Equal(t, "one", a.Labels["profile"], "borrowed informer objects must remain immutable")
	updated := a.DeepCopy()
	updated.Labels["profile"] = "two"
	require.NoError(t, index.index.Update(updated))
	got, err = index.list(t.Context(), nil, sets.New("a-0"))
	require.NoError(t, err)
	require.Equal(t, "two", got[0].Labels["profile"])
	require.NoError(t, index.index.Delete(updated))
	got, err = index.list(t.Context(), nil, sets.New("a-0"))
	require.NoError(t, err)
	require.Empty(t, got)
	got, err = index.list(t.Context(), nil, sets.New("b-0"))
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.Equal(t, "b", got[0].Name)
}

func BenchmarkNamespaceSourceLookup(b *testing.B) {
	for _, count := range []int{1, 64, 512} {
		for _, indexed := range []bool{false, true} {
			b.Run(fmt.Sprintf("indexed=%t/namespaces=%d", indexed, count), func(b *testing.B) {
				scheme := runtime.NewScheme()
				require.NoError(b, capsulev1beta2.AddToScheme(scheme))
				objects := []client.Object{&capsulev1beta2.TenantResource{Namespace: "a-0", Name: "source"}}
				for i := range 1000 {
					objects = append(objects, &capsulev1beta2.TenantResource{Namespace: fmt.Sprintf("b-%d", i), Name: "unrelated"})
				}
				reader := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objects...).Build()
				var index *namespaceSourceIndex
				if indexed {
					index = testNamespaceSourceIndex(b, objects...)
				}
				namespaces := sets.New[string]()
				for i := range count {
					namespaces.Insert(fmt.Sprintf("a-%d", i))
				}
				b.ReportAllocs()
				for b.Loop() {
					result, err := index.list(b.Context(), reader, namespaces)
					if err != nil || len(result) != 1 || result[0].Namespace != "a-0" {
						b.Fatalf("lookup: %v %v", result, err)
					}
				}
			})
		}
	}
}
