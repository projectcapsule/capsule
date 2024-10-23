// Copyright 2020-2023 Project Capsule Authors.
// SPDX-License-Identifier: Apache-2.0

package v1beta2

import (
	"reflect"
	"testing"

	"github.com/projectcapsule/capsule/pkg/api"
	rbacv1 "k8s.io/api/rbac/v1"
)

var tenant = &Tenant{
	Spec: TenantSpec{
		Owners: []OwnerSpec{
			{
				Kind:         "User",
				Name:         "user1",
				ClusterRoles: []string{"cluster-admin", "read-only"},
			},
			{
				Kind:         "Group",
				Name:         "group1",
				ClusterRoles: []string{"edit"},
			},
			{
				Kind:         ServiceAccountOwner,
				Name:         "service",
				ClusterRoles: []string{"read-only"},
			},
		},
		AdditionalRoleBindings: []api.AdditionalRoleBindingsSpec{
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
					{
						Kind: "User",
						Name: "user3",
					},
					{
						Kind: "Group",
						Name: "group1",
					},
				},
			},
			{
				ClusterRoleName: "deployer",
				Subjects: []rbacv1.Subject{
					{
						Kind: "ServiceAccount",
						Name: "system:serviceaccount:argocd:argo-operator",
					},
				},
			},
		},
	},
}

// TestGetClusterRolePermissions tests the GetClusterRolePermissions function
func TestGetSubjectsByClusterRoles(t *testing.T) {
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

	// Call the function to test
	permissions := tenant.GetSubjectsByClusterRoles(nil)

	if !reflect.DeepEqual(permissions, expected) {
		t.Errorf("Expected %v, but got %v", expected, permissions)
	}

	// Ignore SubjectTypes (Ignores ServiceAccounts)
	ignored := tenant.GetSubjectsByClusterRoles([]OwnerKind{"ServiceAccount"})
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

	if !reflect.DeepEqual(ignored, expectedIgnored) {
		t.Errorf("Expected %v, but got %v", expectedIgnored, ignored)
	}

}

func TestGetClusterRolesBySubject(t *testing.T) {

	expected := map[string]map[string]api.TenantSubjectRoles{
		"User": {
			"user1": {
				ClusterRoles: []string{"cluster-admin", "read-only"},
			},
			"user2": {
				ClusterRoles: []string{"developer"},
			},
			"user3": {
				ClusterRoles: []string{"cluster-admin"},
			},
		},
		"Group": {
			"group1": {
				ClusterRoles: []string{"edit", "developer", "cluster-admin"},
			},
		},
		"ServiceAccount": {
			"service": {
				ClusterRoles: []string{"read-only"},
			},
			"system:serviceaccount:argocd:argo-operator": {
				ClusterRoles: []string{"deployer"},
			},
		},
	}

	permissions := tenant.GetClusterRolesBySubject(nil)
	if !reflect.DeepEqual(permissions, expected) {
		t.Errorf("Expected %v, but got %v", expected, permissions)
	}

	delete(expected, "ServiceAccount")
	ignored := tenant.GetClusterRolesBySubject([]OwnerKind{"ServiceAccount"})

	if !reflect.DeepEqual(ignored, expected) {
		t.Errorf("Expected %v, but got %v", expected, ignored)
	}
}

// Helper function to run tests
func TestMain(t *testing.M) {
	t.Run()
}

// permissionsEqual checks the equality of two TenantPermission structs.
func permissionsEqual(a, b api.TenantSubjectRoles) bool {
	if a.Kind != b.Kind {
		return false
	}
	if len(a.ClusterRoles) != len(b.ClusterRoles) {
		return false
	}

	// Create a map to count occurrences of cluster roles
	counts := make(map[string]int)
	for _, role := range a.ClusterRoles {
		counts[role]++
	}
	for _, role := range b.ClusterRoles {
		counts[role]--
		if counts[role] < 0 {
			return false // More occurrences in b than in a
		}
	}
	return true
}
