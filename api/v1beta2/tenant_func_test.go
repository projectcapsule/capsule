// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package v1beta2_test

import (
	"reflect"
	"testing"

	rbacv1 "k8s.io/api/rbac/v1"

	"github.com/projectcapsule/capsule/api/v1beta2"
	capsulerbac "github.com/projectcapsule/capsule/pkg/api/rbac"
)

func testTenant() *v1beta2.Tenant {
	return &v1beta2.Tenant{
		Spec: v1beta2.TenantSpec{
			AdditionalRoleBindings: []capsulerbac.AdditionalRoleBindingsSpec{
				{
					ClusterRoleName: "developer",
					Subjects: []rbacv1.Subject{
						{Kind: "User", Name: "user2"},
						{Kind: "Group", Name: "group1"},
					},
				},
				{
					ClusterRoleName: "cluster-admin",
					Subjects: []rbacv1.Subject{
						{Kind: "User", Name: "user3"},
						{Kind: "Group", Name: "group1"},
					},
				},
				{
					ClusterRoleName: "deployer",
					Subjects: []rbacv1.Subject{
						{Kind: "ServiceAccount", Name: "system:serviceaccount:argocd:argo-operator"},
					},
				},
			},
		},
		Status: v1beta2.TenantStatus{
			Owners: capsulerbac.OwnerStatusListSpec{
				{
					UserSpec: capsulerbac.UserSpec{
						Kind: capsulerbac.UserOwner,
						Name: "user1",
					},
					ClusterRoles: []string{"cluster-admin", "read-only"},
				},
				{
					UserSpec: capsulerbac.UserSpec{
						Kind: capsulerbac.GroupOwner,
						Name: "group1",
					},
					ClusterRoles: []string{"edit"},
				},
				{
					UserSpec: capsulerbac.UserSpec{
						Kind: capsulerbac.ServiceAccountOwner,
						Name: "service",
					},
					ClusterRoles: []string{"read-only"},
				},
			},
		},
	}

}

func TestGetSubjectsByClusterRoles(t *testing.T) {
	t.Parallel()

	tenant := testTenant()

	expected := map[string][]rbacv1.Subject{
		"cluster-admin": {
			{Kind: "User", Name: "user1"},
			{Kind: "User", Name: "user3"},
			{Kind: "Group", Name: "group1"},
		},
		"read-only": {
			{Kind: "User", Name: "user1"},
			{Kind: "ServiceAccount", Name: "service"},
		},
		"edit": {
			{Kind: "Group", Name: "group1"},
		},
		"developer": {
			{Kind: "User", Name: "user2"},
			{Kind: "Group", Name: "group1"},
		},
		"deployer": {
			{Kind: "ServiceAccount", Name: "system:serviceaccount:argocd:argo-operator"},
		},
	}

	got := tenant.GetSubjectsByClusterRoles(nil)
	if !reflect.DeepEqual(got, expected) {
		t.Fatalf("expected %#v\n got %#v", expected, got)
	}

	expectedIgnored := map[string][]rbacv1.Subject{
		"cluster-admin": {
			{Kind: "User", Name: "user1"},
			{Kind: "User", Name: "user3"},
			{Kind: "Group", Name: "group1"},
		},
		"read-only": {
			{Kind: "User", Name: "user1"},
		},
		"edit": {
			{Kind: "Group", Name: "group1"},
		},
		"developer": {
			{Kind: "User", Name: "user2"},
			{Kind: "Group", Name: "group1"},
		},
	}

	gotIgnored := tenant.GetSubjectsByClusterRoles([]capsulerbac.OwnerKind{capsulerbac.ServiceAccountOwner})
	if !reflect.DeepEqual(gotIgnored, expectedIgnored) {
		t.Fatalf("expected %#v\n got %#v", expectedIgnored, gotIgnored)
	}
}

func TestGetClusterRolesBySubjectSorted(t *testing.T) {
	t.Parallel()

	tenant := &v1beta2.Tenant{
		Spec: v1beta2.TenantSpec{
			AdditionalRoleBindings: []capsulerbac.AdditionalRoleBindingsSpec{
				{
					ClusterRoleName: "deployer",
					Subjects: []rbacv1.Subject{
						{
							Kind: "ServiceAccount",
							Name: "system:serviceaccount:argocd:argo-operator",
						},
					},
				},
				{
					ClusterRoleName: "developer",
					Subjects: []rbacv1.Subject{
						{
							Kind: "Group",
							Name: "group1",
						},
					},
				},
			},
		},
		Status: v1beta2.TenantStatus{
			Owners: capsulerbac.OwnerStatusListSpec{
				{
					UserSpec: capsulerbac.UserSpec{
						Kind: capsulerbac.UserOwner,
						Name: "user1",
					},
					ClusterRoles: []string{"cluster-admin", "read-only"},
				},
				{
					UserSpec: capsulerbac.UserSpec{
						Kind: capsulerbac.UserOwner,
						Name: "user2",
					},
					ClusterRoles: []string{"developer"},
				},
				{
					UserSpec: capsulerbac.UserSpec{
						Kind: capsulerbac.UserOwner,
						Name: "user3",
					},
					ClusterRoles: []string{"cluster-admin"},
				},
				{
					UserSpec: capsulerbac.UserSpec{
						Kind: capsulerbac.GroupOwner,
						Name: "group1",
					},
					ClusterRoles: []string{"edit", "developer", "cluster-admin"},
				},
				{
					UserSpec: capsulerbac.UserSpec{
						Kind: capsulerbac.ServiceAccountOwner,
						Name: "service",
					},
					ClusterRoles: []string{"read-only"},
				},
			},
		},
	}

	expected := []capsulerbac.SubjectRoles{
		{Kind: "Group", Name: "group1", Roles: []string{"cluster-admin", "developer", "edit"}},
		{Kind: "ServiceAccount", Name: "service", Roles: []string{"read-only"}},
		{Kind: "ServiceAccount", Name: "system:serviceaccount:argocd:argo-operator", Roles: []string{"deployer"}},
		{Kind: "User", Name: "user1", Roles: []string{"cluster-admin", "read-only"}},
		{Kind: "User", Name: "user2", Roles: []string{"developer"}},
		{Kind: "User", Name: "user3", Roles: []string{"cluster-admin"}},
	}

	t.Run("includes all kinds and is deterministic, deduped and sorted", func(t *testing.T) {
		t.Parallel()

		got := tenant.GetClusterRolesBySubject(nil)
		if !reflect.DeepEqual(got, expected) {
			t.Fatalf("expected %#v\n got %#v", expected, got)
		}
	})

	t.Run("ignores ServiceAccount kind", func(t *testing.T) {
		t.Parallel()

		got := tenant.GetClusterRolesBySubject([]capsulerbac.OwnerKind{capsulerbac.ServiceAccountOwner})

		expectedIgnored := []capsulerbac.SubjectRoles{
			{Kind: "Group", Name: "group1", Roles: []string{"cluster-admin", "developer", "edit"}},
			{Kind: "User", Name: "user1", Roles: []string{"cluster-admin", "read-only"}},
			{Kind: "User", Name: "user2", Roles: []string{"developer"}},
			{Kind: "User", Name: "user3", Roles: []string{"cluster-admin"}},
		}

		if !reflect.DeepEqual(got, expectedIgnored) {
			t.Fatalf("expected %#v\n got %#v", expectedIgnored, got)
		}
	})

	t.Run("empty tenant yields empty slice", func(t *testing.T) {
		t.Parallel()

		empty := &v1beta2.Tenant{}
		got := empty.GetClusterRolesBySubject(nil)
		if len(got) != 0 {
			t.Fatalf("expected empty, got %#v", got)
		}
	})
}
