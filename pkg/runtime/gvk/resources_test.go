// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package gvk_test

import (
	"reflect"
	"testing"

	"github.com/projectcapsule/capsule/pkg/runtime/gvk"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

func TestNamespacedListableResources(t *testing.T) {
	t.Parallel()

	got, err := gvk.NamespacedListableResources([]*metav1.APIResourceList{{
		GroupVersion: "apps/v1",
		APIResources: []metav1.APIResource{
			{Name: "deployments", Namespaced: true, Verbs: metav1.Verbs{"list", "patch"}},
			{Name: "deployments/status", Namespaced: true, Verbs: metav1.Verbs{"list", "patch"}},
			{Name: "daemonsets", Namespaced: true, Verbs: metav1.Verbs{"list"}},
			{Name: "statefulsets", Namespaced: true, Verbs: metav1.Verbs{"list", "update"}},
			{Name: "clusterthings", Namespaced: false, Verbs: metav1.Verbs{"list", "patch"}},
			{Name: "deployments", Namespaced: true, Verbs: metav1.Verbs{"list", "patch"}},
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
