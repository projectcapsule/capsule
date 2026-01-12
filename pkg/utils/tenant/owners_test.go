// Copyright 2020-2025 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package tenant_test

import (
	"reflect"
	"testing"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/utils/tenant"
)

func TestGetOwnersWithKinds_EmptyOwners(t *testing.T) {
	tnt := &capsulev1beta2.Tenant{}

	owners := tenant.GetOwnersWithKinds(tnt)

	if owners == nil {
		t.Fatalf("expected empty slice, got nil")
	}
	if len(owners) != 0 {
		t.Fatalf("expected empty slice, got %v", owners)
	}
}

func TestGetOwnersWithKinds_SingleOwner(t *testing.T) {
	tnt := &capsulev1beta2.Tenant{
		Status: capsulev1beta2.TenantStatus{
			Owners: []capsulev1beta2.OwnerReference{
				{
					Kind: capsulev1beta2.OwnerKindUser,
					Name: "alice",
				},
			},
		},
	}

	owners := tenant.GetOwnersWithKinds(tnt)

	want := []string{"User:alice"}
	if !reflect.DeepEqual(owners, want) {
		t.Fatalf("unexpected owners:\nwant=%v\ngot =%v", want, owners)
	}
}

func TestGetOwnersWithKinds_MultipleOwners_PreservesOrder(t *testing.T) {
	tnt := &capsulev1beta2.Tenant{
		Status: capsulev1beta2.TenantStatus{
			Owners: []capsulev1beta2.OwnerReference{
				{
					Kind: capsulev1beta2.OwnerKindGroup,
					Name: "admins",
				},
				{
					Kind: capsulev1beta2.OwnerKindUser,
					Name: "bob",
				},
			},
		},
	}

	owners := tenant.GetOwnersWithKinds(tnt)

	want := []string{
		"Group:admins",
		"User:bob",
	}
	if !reflect.DeepEqual(owners, want) {
		t.Fatalf("unexpected owners:\nwant=%v\ngot =%v", want, owners)
	}
}

func TestGetOwnersWithKinds_EmptyNameStillIncluded(t *testing.T) {
	tnt := &capsulev1beta2.Tenant{
		Status: capsulev1beta2.TenantStatus{
			Owners: []capsulev1beta2.OwnerReference{
				{
					Kind: capsulev1beta2.OwnerKindUser,
					Name: "",
				},
			},
		},
	}

	owners := tenant.GetOwnersWithKinds(tnt)

	want := []string{"User:"}
	if !reflect.DeepEqual(owners, want) {
		t.Fatalf("unexpected owners:\nwant=%v\ngot =%v", want, owners)
	}
}
