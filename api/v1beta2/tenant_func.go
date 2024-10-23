// Copyright 2020-2023 Project Capsule Authors.
// SPDX-License-Identifier: Apache-2.0

package v1beta2

import (
	"slices"
	"sort"

	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"

	"github.com/projectcapsule/capsule/pkg/api"
)

func (in *Tenant) IsFull() bool {
	// we don't have limits on assigned Namespaces
	if in.Spec.NamespaceOptions == nil || in.Spec.NamespaceOptions.Quota == nil {
		return false
	}

	return len(in.Status.Namespaces) >= int(*in.Spec.NamespaceOptions.Quota)
}

func (in *Tenant) AssignNamespaces(namespaces []corev1.Namespace) {
	var l []string

	for _, ns := range namespaces {
		if ns.Status.Phase == corev1.NamespaceActive {
			l = append(l, ns.GetName())
		}
	}

	sort.Strings(l)

	in.Status.Namespaces = l
	in.Status.Size = uint(len(l))
}

func (in *Tenant) GetOwnerProxySettings(name string, kind OwnerKind) []ProxySettings {
	return in.Spec.Owners.FindOwner(name, kind).ProxyOperations
}

// GetClusterRolePermissions returns a map where the clusterRole is the key
// and the value is a list of permission subjects (kind and name) that reference that role.
// These mappings are gathered from the owners and additionalRolebindings spec.
func (in *Tenant) GetSubjectsByClusterRoles(ignoreOwnerKind []OwnerKind) (rolePerms map[string][]rbacv1.Subject) {
	rolePerms = make(map[string][]rbacv1.Subject)

	// Helper to add permissions for a given clusterRole
	addPermission := func(clusterRole string, permission rbacv1.Subject) {
		if _, exists := rolePerms[clusterRole]; !exists {
			rolePerms[clusterRole] = []rbacv1.Subject{}
		}

		rolePerms[clusterRole] = append(rolePerms[clusterRole], permission)
	}

	// Helper to check if a kind is in the ignoreOwnerKind list
	isIgnoredKind := func(kind string) bool {
		for _, ignored := range ignoreOwnerKind {
			if kind == ignored.String() {
				return true
			}
		}

		return false
	}

	// Process owners
	for _, owner := range in.Spec.Owners {
		if !isIgnoredKind(owner.Kind.String()) {
			for _, clusterRole := range owner.ClusterRoles {
				perm := rbacv1.Subject{
					Name: owner.Name,
					Kind: owner.Kind.String(),
				}
				addPermission(clusterRole, perm)
			}
		}
	}

	// Process additional role bindings
	for _, role := range in.Spec.AdditionalRoleBindings {
		for _, subject := range role.Subjects {
			if !isIgnoredKind(subject.Kind) {
				perm := rbacv1.Subject{
					Name: subject.Name,
					Kind: subject.Kind,
				}
				addPermission(role.ClusterRoleName, perm)
			}
		}
	}

	return
}

// Get the permissions for a tenant ordered by groups and users.
func (in *Tenant) GetClusterRolesBySubject(ignoreOwnerKind []OwnerKind) (maps map[string]map[string]api.TenantSubjectRoles) {
	maps = make(map[string]map[string]api.TenantSubjectRoles)

	// Initialize a nested map for kind ("User", "Group") and name
	initNestedMap := func(kind string) {
		if _, exists := maps[kind]; !exists {
			maps[kind] = make(map[string]api.TenantSubjectRoles)
		}
	}
	// Helper to check if a kind is in the ignoreOwnerKind list
	isIgnoredKind := func(kind string) bool {
		for _, ignored := range ignoreOwnerKind {
			if kind == ignored.String() {
				return true
			}
		}

		return false
	}

	// Process owners
	for _, owner := range in.Spec.Owners {
		if !isIgnoredKind(owner.Kind.String()) {
			initNestedMap(owner.Kind.String())

			if perm, exists := maps[owner.Kind.String()][owner.Name]; exists {
				// If the permission entry already exists, append cluster roles
				perm.ClusterRoles = append(perm.ClusterRoles, owner.ClusterRoles...)
				maps[owner.Kind.String()][owner.Name] = perm
			} else {
				// Create a new permission entry
				maps[owner.Kind.String()][owner.Name] = api.TenantSubjectRoles{
					ClusterRoles: owner.ClusterRoles,
				}
			}
		}
	}

	// Process additional role bindings
	for _, role := range in.Spec.AdditionalRoleBindings {
		for _, subject := range role.Subjects {
			if !isIgnoredKind(subject.Kind) {
				initNestedMap(subject.Kind)

				if perm, exists := maps[subject.Kind][subject.Name]; exists {
					// If the permission entry already exists, append cluster roles
					perm.ClusterRoles = append(perm.ClusterRoles, role.ClusterRoleName)
					maps[subject.Kind][subject.Name] = perm
				} else {
					// Create a new permission entry
					maps[subject.Kind][subject.Name] = api.TenantSubjectRoles{
						ClusterRoles: []string{role.ClusterRoleName},
					}
				}
			}
		}
	}

	// Remove duplicates from cluster roles in both maps
	for kind, nameMap := range maps {
		for name, perm := range nameMap {
			perm.ClusterRoles = slices.Compact(perm.ClusterRoles)
			maps[kind][name] = perm
		}
	}

	return maps
}
