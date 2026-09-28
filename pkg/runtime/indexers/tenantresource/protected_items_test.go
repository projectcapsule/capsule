// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package tenantresource

import (
	"fmt"
	"testing"

	"sigs.k8s.io/controller-runtime/pkg/client"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	apiruntime "github.com/projectcapsule/capsule/pkg/api/runtime"
)

func TestProtectedItems(t *testing.T) {
	for _, tc := range []struct {
		name               string
		created, protected bool
		policy             *apiruntime.ResourceTemplatePolicy
	}{
		{name: "legacy created", created: true, protected: true},
		{name: "legacy adopted"},
		{name: "protected adopted", policy: &apiruntime.ResourceTemplatePolicy{}, protected: true},
		{name: "unprotected created", policy: &apiruntime.ResourceTemplatePolicy{Protect: new(false)}, created: true},
	} {
		for _, global := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/global=%t", tc.name, global), func(t *testing.T) {
				items := meta.ProcessedItems{{Version: "v1", Kind: "ConfigMap", Namespace: "tenant-a", Name: "item", Created: tc.created, Policy: tc.policy}}
				var obj client.Object
				if global {
					obj = &capsulev1beta2.GlobalTenantResource{Status: capsulev1beta2.GlobalTenantResourceStatus{ProcessedItems: items}}
				} else {
					obj = &capsulev1beta2.TenantResource{Status: capsulev1beta2.TenantResourceStatus{ProcessedItems: items}}
				}
				index := ProtectedItems{Obj: obj}
				keys := index.Func()(obj)
				if (len(keys) == 1) != tc.protected {
					t.Fatalf("unexpected protected keys: %v", keys)
				}
				if tc.protected && keys[0] != items[0].GetGVKKey("") {
					t.Fatal("index lost resource namespace")
				}
				items[0].ClusterScoped = true
				if keys = index.Func()(obj); tc.protected && keys[0] != processedItemKey(items[0]) {
					t.Fatal("cluster-scoped key contains tracking namespace")
				}
				items[0].Policy = &apiruntime.ResourceTemplatePolicy{Protect: new(false)}
				if keys = index.Func()(obj); len(keys) != 0 {
					t.Fatal("policy update did not remove protection")
				}
			})
		}
	}
}

func BenchmarkReplicationProtectionIndex(b *testing.B) {
	for _, n := range []int{1, 100, 1000} {
		obj := &capsulev1beta2.TenantResource{}
		for i := range n {
			obj.Status.ProcessedItems = append(obj.Status.ProcessedItems, meta.ObjectReferenceStatus{Version: "v1", Kind: "ConfigMap", Namespace: fmt.Sprintf("ns-%d", i), Name: "item", Created: true})
		}
		for _, policy := range []bool{false, true} {
			b.Run(fmt.Sprintf("items=%d/policy=%t", n, policy), func(b *testing.B) {
				resource := obj.DeepCopy()
				var index client.IndexerFunc = (NamespacedCreatedItems{}).Func()
				if policy {
					index = (ProtectedItems{}).Func()
					for i := range resource.Status.ProcessedItems {
						resource.Status.ProcessedItems[i].Policy = &apiruntime.ResourceTemplatePolicy{}
					}
				}
				b.ReportAllocs()
				for b.Loop() {
					if got := index(resource); len(got) != n {
						b.Fatal("missing protection keys")
					}
				}
			})
		}
	}
}
