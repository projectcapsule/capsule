// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package tenant_test

import (
	"reflect"
	"testing"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api"
	"github.com/projectcapsule/capsule/pkg/tenant"
)

func TestGetOwnersWithKinds_EmptyOwners(t *testing.T) {
	tnt := &capsulev1beta2.Tenant{}

	owners := tenant.GetOwnersWithKinds(tnt)

	if owners != nil {
		t.Fatalf("expected empty slice, got nil")
	}
}

func TestGetOwnersWithKinds_SingleOwner(t *testing.T) {

	tnt := &capsulev1beta2.Tenant{
		Status: capsulev1beta2.TenantStatus{
			Owners: []api.CoreOwnerSpec{
				{
					UserSpec: api.UserSpec{
						Kind: api.UserOwner,
						Name: "alice",
					},
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
			Owners: []api.CoreOwnerSpec{
				{
					UserSpec: api.UserSpec{
						Kind: api.GroupOwner,
						Name: "admins",
					},
				},
				{
					UserSpec: api.UserSpec{
						Kind: api.UserOwner,
						Name: "bob",
					},
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
			Owners: []api.CoreOwnerSpec{
				{
					UserSpec: api.UserSpec{
						Kind: api.UserOwner,
						Name: "",
					},
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
