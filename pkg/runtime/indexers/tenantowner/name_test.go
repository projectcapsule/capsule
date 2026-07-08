// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package tenantowner_test

import (
	"reflect"
	"testing"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/rbac"
	"github.com/projectcapsule/capsule/pkg/runtime/indexers/tenantowner"
)

func TestOwnerNameReference(t *testing.T) {
	t.Parallel()

	idx := tenantowner.OwnerNameReference{}
	if idx.Object() == nil || idx.Field() != tenantowner.NameIndexerFieldName {
		t.Fatalf("unexpected object/field")
	}

	if got := idx.Func()(&capsulev1beta2.TenantOwner{Spec: capsulev1beta2.TenantOwnerSpec{
		CoreOwnerSpec: rbac.CoreOwnerSpec{UserSpec: rbac.UserSpec{Name: "alice"}},
	}}); !reflect.DeepEqual(got, []string{"alice"}) {
		t.Fatalf("Func() = %#v, want alice", got)
	}
	if got := idx.Func()(&capsulev1beta2.TenantOwner{}); got != nil {
		t.Fatalf("Func() = %#v, want nil for empty name", got)
	}
}
