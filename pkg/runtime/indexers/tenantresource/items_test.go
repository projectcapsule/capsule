// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package tenantresource

import (
	"reflect"
	"testing"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/runtime/gvk"
)

func TestCreatedItemsIndexersUseAdmissionKeyForClusterScopedItems(t *testing.T) {
	t.Parallel()

	item := meta.ObjectReferenceStatus{
		ResourceID: gvk.ResourceID{
			Group:     "rbac.authorization.k8s.io",
			Version:   "v1",
			Kind:      "ClusterRole",
			Namespace: "tenant-a",
			Name:      "example",
		},
		ObjectReferenceStatusCondition: meta.ObjectReferenceStatusCondition{
			Created:       true,
			ClusterScoped: true,
		},
	}

	want := []string{
		gvk.ResourceID{
			Group:   "rbac.authorization.k8s.io",
			Version: "v1",
			Kind:    "ClusterRole",
			Name:    "example",
		}.GetGVKKey(""),
	}

	t.Run("global", func(t *testing.T) {
		t.Parallel()

		idx := GlobalCreatedItems{}
		obj := &capsulev1beta2.GlobalTenantResource{}
		obj.Status.ProcessedItems = meta.ProcessedItems{item}

		if got := idx.Func()(obj); !reflect.DeepEqual(got, want) {
			t.Fatalf("unexpected keys\nwant=%#v\ngot =%#v", want, got)
		}
	})

	t.Run("namespaced", func(t *testing.T) {
		t.Parallel()

		idx := NamespacedCreatedItems{}
		obj := &capsulev1beta2.TenantResource{}
		obj.Status.ProcessedItems = meta.ProcessedItems{item}

		if got := idx.Func()(obj); !reflect.DeepEqual(got, want) {
			t.Fatalf("unexpected keys\nwant=%#v\ngot =%#v", want, got)
		}
	})
}

func TestProcessedItemKeyKeepsNamespaceForNamespacedItems(t *testing.T) {
	t.Parallel()

	item := meta.ObjectReferenceStatus{
		ResourceID: gvk.ResourceID{
			Version:   "v1",
			Kind:      "Secret",
			Namespace: "tenant-a",
			Name:      "example",
		},
	}

	want := item.GetGVKKey("")

	if got := processedItemKey(item); got != want {
		t.Fatalf("unexpected key: want %q, got %q", want, got)
	}
}
