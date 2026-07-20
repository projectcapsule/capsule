// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package processor_test

import (
	"testing"

	"github.com/projectcapsule/capsule/pkg/api/processor"
	"github.com/projectcapsule/capsule/pkg/runtime/gvk"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func TestAccumulatorAdd(t *testing.T) {
	t.Parallel()

	resource := gvk.ResourceID{Version: "v1", Kind: "ConfigMap", Namespace: "tenant-a", Name: "settings"}
	obj := processor.AccumulatorObject{
		Origin: gvk.TenantResourceIDWithOrigin{TenantResourceID: gvk.TenantResourceID{Tenant: "tenant-a"}, Origin: "resource-a"},
		Object: &unstructured.Unstructured{},
	}

	processor.AccumulatorAdd(nil, resource, obj)

	acc := processor.Accumulator{}
	processor.AccumulatorAdd(acc, resource, obj)
	processor.AccumulatorAdd(acc, resource, obj)

	entry := acc[resource.GetKey("")]
	if entry == nil {
		t.Fatalf("AccumulatorAdd() did not create entry")
	}
	if entry.Objects == nil || len(*entry.Objects) != 2 {
		t.Fatalf("AccumulatorAdd() objects = %#v, want two objects", entry.Objects)
	}

	entry.Objects = nil
	processor.AccumulatorAdd(acc, resource, obj)
	if entry.Objects == nil || len(*entry.Objects) != 1 {
		t.Fatalf("AccumulatorAdd() did not initialize nil object slice")
	}
}
