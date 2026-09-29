// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package gvk_test

import (
	"fmt"
	"reflect"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/projectcapsule/capsule/pkg/runtime/gvk"
)

func TestNamespacedListableResources(t *testing.T) {
	t.Parallel()

	got, err := gvk.NamespacedListableResources([]*metav1.APIResourceList{{
		GroupVersion: "apps/v1",
		APIResources: []metav1.APIResource{
			{Name: "deployments", Namespaced: true, Verbs: metav1.Verbs{"get", "list", "delete", "patch"}},
			{Name: "deployments/status", Namespaced: true, Verbs: metav1.Verbs{"get", "list", "delete", "patch"}},
			{Name: "daemonsets", Namespaced: true, Verbs: metav1.Verbs{"list"}},
			{Name: "statefulsets", Namespaced: true, Verbs: metav1.Verbs{"get", "list", "delete", "patch", "update"}},
			{Name: "clusterthings", Namespaced: false, Verbs: metav1.Verbs{"get", "list", "delete", "patch"}},
			{Name: "deployments", Namespaced: true, Verbs: metav1.Verbs{"get", "list", "delete", "patch"}},
		},
	}})
	if err != nil {
		t.Fatalf("NamespacedListableResources() unexpected error: %v", err)
	}

	want := []schema.GroupVersionResource{
		{Group: "apps", Version: "v1", Resource: "deployments"},
		{Group: "apps", Version: "v1", Resource: "statefulsets"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("NamespacedListableResources() = %#v, want %#v", got, want)
	}

	if _, err := gvk.NamespacedListableResources([]*metav1.APIResourceList{{GroupVersion: "not/a/group/version"}}); err == nil {
		t.Fatalf("NamespacedListableResources() expected parse error")
	}
}

func TestNamespacedListableResourcesRequiresCleanupVerbs(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name  string
		verbs metav1.Verbs
		want  bool
	}{
		{name: "supported", verbs: metav1.Verbs{"get", "list", "delete", "patch"}, want: true},
		{name: "missing list", verbs: metav1.Verbs{"get", "delete", "patch"}},
		{name: "missing get", verbs: metav1.Verbs{"list", "delete", "patch"}},
		{name: "missing delete", verbs: metav1.Verbs{"get", "list", "patch", "update"}},
		{name: "deletecollection is not delete", verbs: metav1.Verbs{"get", "list", "deletecollection", "patch"}},
		{name: "update is not patch", verbs: metav1.Verbs{"get", "list", "delete", "update"}},
		{name: "empty verbs", verbs: metav1.Verbs{}},
		{name: "nil verbs"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got, err := gvk.NamespacedListableResources([]*metav1.APIResourceList{{
				GroupVersion: "example.com/v1",
				APIResources: []metav1.APIResource{{Name: "widgets", Namespaced: true, Verbs: tt.verbs}},
			}})
			if err != nil {
				t.Fatal(err)
			}
			if (len(got) == 1) != tt.want || len(got) > 1 {
				t.Fatalf("selected resources = %v, want selected = %v", got, tt.want)
			}
		})
	}
}

func BenchmarkNamespacedListableResources(b *testing.B) {
	for _, count := range []int{30, 300} {
		b.Run(fmt.Sprintf("resources=%d", count), func(b *testing.B) {
			resources := &metav1.APIResourceList{GroupVersion: "example.com/v1"}
			for i := range count {
				resources.APIResources = append(resources.APIResources, metav1.APIResource{
					Name: fmt.Sprintf("objects%d", i), Namespaced: true,
					Verbs: metav1.Verbs{"create", "delete", "deletecollection", "get", "list", "patch", "update", "watch"},
				})
			}
			input := []*metav1.APIResourceList{resources}
			b.ReportAllocs()
			for b.Loop() {
				got, err := gvk.NamespacedListableResources(input)
				if err != nil || len(got) != count {
					b.Fatalf("selected %d resources, want %d: %v", len(got), count, err)
				}
			}
		})
	}
}

func TestSupportsVerb(t *testing.T) {
	t.Parallel()

	if !gvk.SupportsVerb(metav1.Verbs{"get", "list"}, "list") {
		t.Fatalf("SupportsVerb() = false, want true")
	}
	if gvk.SupportsVerb(metav1.Verbs{"get"}, "list") {
		t.Fatalf("SupportsVerb() = true, want false")
	}
}

func TestResourceIDHelpers(t *testing.T) {
	t.Parallel()

	u := &unstructured.Unstructured{}
	u.SetGroupVersionKind(schema.GroupVersionKind{Group: "apps", Version: "v1", Kind: "Deployment"})
	u.SetNamespace("tenant-a")
	u.SetName("api")

	id := gvk.NewResourceID(u, "tenant", "origin")
	if id.GetName() != "api" || id.GetNamespace() != "tenant-a" {
		t.Fatalf("NewResourceID() identity = %#v", id)
	}
	if got := id.GetGVK(); got != (schema.GroupVersionKind{Group: "apps", Version: "v1", Kind: "Deployment"}) {
		t.Fatalf("GetGVK() = %#v", got)
	}
	if got := id.GetGVKKey("/"); got != "apps/v1/Deployment/tenant-a/api/" {
		t.Fatalf("GetGVKKey() = %q", got)
	}
	if got := id.GetKey("/"); got != "apps/v1/Deployment/tenant-a/api/tenant/origin/" {
		t.Fatalf("GetKey() = %q", got)
	}
	if got := id.FieldOwner(""); got != "tenant-a/tenant/origin/" {
		t.Fatalf("FieldOwner() = %q", got)
	}
}
