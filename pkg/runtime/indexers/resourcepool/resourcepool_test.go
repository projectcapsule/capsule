// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package resourcepool_test

import (
	"reflect"
	"testing"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/runtime/indexers/resourcepool"
	"k8s.io/apimachinery/pkg/types"
)

func TestResourcePoolIndexers(t *testing.T) {
	t.Parallel()

	namespaces := resourcepool.NamespacesReference{Obj: &capsulev1beta2.ResourcePool{}}
	if namespaces.Object() == nil || namespaces.Field() != ".status.namespaces" {
		t.Fatalf("unexpected namespace indexer object/field")
	}
	if got := namespaces.Func()(&capsulev1beta2.ResourcePool{Status: capsulev1beta2.ResourcePoolStatus{Namespaces: []string{"a", "b"}}}); !reflect.DeepEqual(got, []string{"a", "b"}) {
		t.Fatalf("NamespacesReference.Func() = %#v", got)
	}

	poolUID := resourcepool.PoolUIDReference{Obj: &capsulev1beta2.ResourcePoolClaim{}}
	if poolUID.Object() == nil || poolUID.Field() != ".status.pool.uid" {
		t.Fatalf("unexpected pool UID indexer object/field")
	}
	if got := poolUID.Func()(&capsulev1beta2.ResourcePoolClaim{Status: capsulev1beta2.ResourcePoolClaimStatus{
		Pool: meta.LocalRFC1123ObjectReferenceWithUID{UID: types.UID("pool-uid")},
	}}); !reflect.DeepEqual(got, []string{"pool-uid"}) {
		t.Fatalf("PoolUIDReference.Func() = %#v", got)
	}
}
