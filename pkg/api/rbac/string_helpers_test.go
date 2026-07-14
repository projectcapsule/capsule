// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package rbac_test

import (
	"reflect"
	"testing"

	"github.com/projectcapsule/capsule/pkg/api/rbac"
)

func TestRBACStringHelpers(t *testing.T) {
	t.Parallel()

	if got := rbac.OwnerKind("CustomOwner").String(); got != "CustomOwner" {
		t.Fatalf("OwnerKind.String() = %q", got)
	}
	if got := rbac.UserKind("CustomUser").String(); got != "CustomUser" {
		t.Fatalf("UserKind.String() = %q", got)
	}
	if got := rbac.ProxyOperation("Watch").String(); got != "Watch" {
		t.Fatalf("ProxyOperation.String() = %q", got)
	}
	if got := rbac.ProxyServiceKind("Tenants").String(); got != "Tenants" {
		t.Fatalf("ProxyServiceKind.String() = %q", got)
	}
}

func TestOwnerListSpecOwnershipAndStatusConversion(t *testing.T) {
	t.Parallel()

	owners := rbac.OwnerListSpec{
		{CoreOwnerSpec: rbac.CoreOwnerSpec{UserSpec: rbac.UserSpec{Kind: rbac.UserOwner, Name: "alice"}}},
		{CoreOwnerSpec: rbac.CoreOwnerSpec{UserSpec: rbac.UserSpec{Kind: rbac.GroupOwner, Name: "team-a"}}},
		{CoreOwnerSpec: rbac.CoreOwnerSpec{UserSpec: rbac.UserSpec{Kind: rbac.ServiceAccountOwner, Name: "system:serviceaccount:tenant-a:builder"}}},
	}

	for _, tt := range []struct {
		name   string
		user   string
		groups []string
		want   bool
	}{
		{name: "direct user", user: "alice", want: true},
		{name: "group", user: "bob", groups: []string{"team-a"}, want: true},
		{name: "service account", user: "system:serviceaccount:tenant-a:builder", want: true},
		{name: "not owner", user: "mallory", groups: []string{"team-b"}, want: false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := owners.IsOwner(tt.user, tt.groups); got != tt.want {
				t.Fatalf("IsOwner() = %t, want %t", got, tt.want)
			}
		})
	}

	wantStatus := rbac.OwnerStatusListSpec{
		{UserSpec: rbac.UserSpec{Kind: rbac.UserOwner, Name: "alice"}},
		{UserSpec: rbac.UserSpec{Kind: rbac.GroupOwner, Name: "team-a"}},
		{UserSpec: rbac.UserSpec{Kind: rbac.ServiceAccountOwner, Name: "system:serviceaccount:tenant-a:builder"}},
	}
	if got := owners.ToStatusOwners(); !reflect.DeepEqual(got, wantStatus) {
		t.Fatalf("ToStatusOwners() = %#v, want %#v", got, wantStatus)
	}
}
