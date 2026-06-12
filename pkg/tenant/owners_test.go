// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package tenant_test

import (
	"reflect"
	"testing"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/rbac"
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
			Owners: []rbac.CoreOwnerSpec{
				{
					UserSpec: rbac.UserSpec{
						Kind: rbac.UserOwner,
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
			Owners: []rbac.CoreOwnerSpec{
				{
					UserSpec: rbac.UserSpec{
						Kind: rbac.GroupOwner,
						Name: "admins",
					},
				},
				{
					UserSpec: rbac.UserSpec{
						Kind: rbac.UserOwner,
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
			Owners: []rbac.CoreOwnerSpec{
				{
					UserSpec: rbac.UserSpec{
						Kind: rbac.UserOwner,
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

func TestValidateTenantOwner(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		owner   rbac.CoreOwnerSpec
		wantErr bool
	}{
		{
			name: "valid service account owner",
			owner: rbac.CoreOwnerSpec{
				UserSpec: rbac.UserSpec{
					Kind: rbac.ServiceAccountOwner,
					Name: "system:serviceaccount:tenant-a:builder",
				},
			},
			wantErr: false,
		},
		{
			name: "invalid service account owner without serviceaccount prefix",
			owner: rbac.CoreOwnerSpec{
				UserSpec: rbac.UserSpec{
					Kind: rbac.ServiceAccountOwner,
					Name: "tenant-a:builder",
				},
			},
			wantErr: true,
		},
		{
			name: "invalid service account owner with missing namespace",
			owner: rbac.CoreOwnerSpec{
				UserSpec: rbac.UserSpec{
					Kind: rbac.ServiceAccountOwner,
					Name: "system:serviceaccount::builder",
				},
			},
			wantErr: true,
		},
		{
			name: "invalid service account owner with missing name",
			owner: rbac.CoreOwnerSpec{
				UserSpec: rbac.UserSpec{
					Kind: rbac.ServiceAccountOwner,
					Name: "system:serviceaccount:tenant-a:",
				},
			},
			wantErr: true,
		},
		{
			name: "invalid service account owner with plain name",
			owner: rbac.CoreOwnerSpec{
				UserSpec: rbac.UserSpec{
					Kind: rbac.ServiceAccountOwner,
					Name: "builder",
				},
			},
			wantErr: true,
		},
		{
			name: "user owner is not validated as service account",
			owner: rbac.CoreOwnerSpec{
				UserSpec: rbac.UserSpec{
					Kind: rbac.UserOwner,
					Name: "alice",
				},
			},
			wantErr: false,
		},
		{
			name: "group owner is not validated as service account",
			owner: rbac.CoreOwnerSpec{
				UserSpec: rbac.UserSpec{
					Kind: rbac.GroupOwner,
					Name: "developers",
				},
			},
			wantErr: false,
		},
		{
			name: "empty user owner name is currently accepted",
			owner: rbac.CoreOwnerSpec{
				UserSpec: rbac.UserSpec{
					Kind: rbac.UserOwner,
					Name: "",
				},
			},
			wantErr: false,
		},
		{
			name: "empty group owner name is currently accepted",
			owner: rbac.CoreOwnerSpec{
				UserSpec: rbac.UserSpec{
					Kind: rbac.GroupOwner,
					Name: "",
				},
			},
			wantErr: false,
		},
		{
			name: "empty service account owner name is rejected",
			owner: rbac.CoreOwnerSpec{
				UserSpec: rbac.UserSpec{

					Kind: rbac.ServiceAccountOwner,
					Name: "",
				},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		tt := tt

		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := tenant.ValidateTenantOwner(tt.owner)

			if tt.wantErr && err == nil {
				t.Fatalf("ValidateTenantOwner() expected error, got nil")
			}

			if !tt.wantErr && err != nil {
				t.Fatalf("ValidateTenantOwner() unexpected error: %v", err)
			}
		})
	}
}
