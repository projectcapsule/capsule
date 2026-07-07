// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package tenantresource_test

import (
	"reflect"
	"testing"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/runtime/gvk"
	"github.com/projectcapsule/capsule/pkg/runtime/indexers/tenantresource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestNamespacedTenantResourceIndexers(t *testing.T) {
	t.Parallel()

	item := meta.ObjectReferenceStatus{
		ResourceID: gvk.ResourceID{
			Version:   "v1",
			Kind:      "ConfigMap",
			Namespace: "tenant-a",
			Name:      "settings",
		},
	}
	wantItemKey := item.GetGVKKey("")

	tr := &capsulev1beta2.TenantResource{
		ObjectMeta: metav1.ObjectMeta{Namespace: "tenant-a"},
		Status: capsulev1beta2.TenantResourceStatus{
			TenantResourceCommonStatus: capsulev1beta2.TenantResourceCommonStatus{
				ServiceAccount: &meta.NamespacedRFC1123ObjectReferenceWithNamespace{Name: "builder"},
				ProcessedItems: meta.ProcessedItems{
					item,
					{ResourceID: gvk.ResourceID{Version: "v1", Kind: "Secret", Namespace: "tenant-a", Name: "token"}},
				},
			},
		},
	}

	if idx := (tenantresource.NamespacedServiceAccount{}); idx.Object() == nil || idx.Field() != tenantresource.ServiceAccountIndexerFieldName {
		t.Fatalf("unexpected namespaced service account object/field")
	} else if got := idx.Func()(tr); !reflect.DeepEqual(got, []string{"tenant-a/builder"}) {
		t.Fatalf("NamespacedServiceAccount.Func() = %#v", got)
	}

	if idx := (tenantresource.NamespacedResourceNamespace{}); idx.Object() == nil || idx.Field() != tenantresource.NamespaceIndexerFieldName {
		t.Fatalf("unexpected namespace object/field")
	} else if got := idx.Func()(tr); !reflect.DeepEqual(got, []string{"tenant-a"}) {
		t.Fatalf("NamespacedResourceNamespace.Func() = %#v", got)
	}

	if idx := (tenantresource.NamespacedProcessedItems{}); idx.Object() == nil || idx.Field() != tenantresource.ProcessedIndexerFieldName {
		t.Fatalf("unexpected processed items object/field")
	} else if got := idx.Func()(tr); len(got) != 2 || got[0] != wantItemKey {
		t.Fatalf("NamespacedProcessedItems.Func() = %#v", got)
	}

	if got := (tenantresource.NamespacedServiceAccount{}).Func()(&capsulev1beta2.TenantResource{}); got != nil {
		t.Fatalf("NamespacedServiceAccount empty = %#v, want nil", got)
	}
	if got := (tenantresource.NamespacedResourceNamespace{}).Func()(&capsulev1beta2.TenantResource{}); got != nil {
		t.Fatalf("NamespacedResourceNamespace empty = %#v, want nil", got)
	}
}

func TestGlobalProcessedItemsIndexer(t *testing.T) {
	t.Parallel()

	item := meta.ObjectReferenceStatus{
		ResourceID: gvk.ResourceID{Version: "v1", Kind: "ConfigMap", Namespace: "tenant-a", Name: "settings"},
	}
	gtr := &capsulev1beta2.GlobalTenantResource{
		Status: capsulev1beta2.GlobalTenantResourceStatus{
			TenantResourceCommonStatus: capsulev1beta2.TenantResourceCommonStatus{
				ProcessedItems: meta.ProcessedItems{item},
			},
		},
	}

	idx := tenantresource.GlobalProcessedItems{}
	if idx.Object() == nil || idx.Field() != tenantresource.ProcessedIndexerFieldName {
		t.Fatalf("unexpected global processed items object/field")
	}
	if got := idx.Func()(gtr); !reflect.DeepEqual(got, []string{item.GetGVKKey("")}) {
		t.Fatalf("GlobalProcessedItems.Func() = %#v", got)
	}
}
