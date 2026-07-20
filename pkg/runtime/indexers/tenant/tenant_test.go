// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package tenant_test

import (
	"reflect"
	"testing"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/rbac"
	tenantindexer "github.com/projectcapsule/capsule/pkg/runtime/indexers/tenant"
)

func TestTenantIndexers(t *testing.T) {
	t.Parallel()

	namespaces := tenantindexer.NamespacesReference{Obj: &capsulev1beta2.Tenant{}}
	if namespaces.Object() == nil || namespaces.Field() != tenantindexer.NamespaceIndexerFieldName {
		t.Fatalf("unexpected namespace indexer object/field")
	}
	if got := namespaces.Func()(&capsulev1beta2.Tenant{Status: capsulev1beta2.TenantStatus{Namespaces: []string{"a", "b"}}}); !reflect.DeepEqual(got, []string{"a", "b"}) {
		t.Fatalf("NamespacesReference.Func() = %#v", got)
	}

	owners := tenantindexer.OwnerReference{}
	if owners.Object() == nil || owners.Field() != tenantindexer.OwnerKindIndexerFieldName {
		t.Fatalf("unexpected owner indexer object/field")
	}
	if got := owners.Func()(&capsulev1beta2.Tenant{Status: capsulev1beta2.TenantStatus{Owners: rbac.OwnerStatusListSpec{{
		UserSpec: rbac.UserSpec{Kind: rbac.UserOwner, Name: "alice"},
	}}}}); !reflect.DeepEqual(got, []string{"User:alice"}) {
		t.Fatalf("OwnerReference.Func() = %#v", got)
	}
}
