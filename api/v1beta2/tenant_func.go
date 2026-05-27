// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package v1beta2

import (
	"context"
	"sort"

	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/projectcapsule/capsule/pkg/api/rbac"
)

func (in *Tenant) GetRoleBindings() []rbac.AdditionalRoleBindingsSpec {
	roleBindings := make([]rbac.AdditionalRoleBindingsSpec, 0, len(in.Spec.AdditionalRoleBindings))

	for _, owner := range in.Status.Owners {
		roleBindings = append(roleBindings, owner.ToAdditionalRolebindings()...)
	}

	roleBindings = append(roleBindings, in.Spec.AdditionalRoleBindings...)

	return roleBindings
}

func (in *Tenant) GetPromotionRoleBindings() []rbac.AdditionalRoleBindingsWithNamespaceSpec {
	roleBindings := make([]rbac.AdditionalRoleBindingsWithNamespaceSpec, 0, len(in.Status.Promotions))

	for _, promotion := range in.Status.Promotions {
		roleBindings = append(roleBindings, promotion.ToAdditionalRolebindings()...)
	}

	return roleBindings
}

func (in *Tenant) IsFull() bool {
	// we don't have limits on assigned Namespaces
	if in.Spec.NamespaceOptions == nil || in.Spec.NamespaceOptions.Quota == nil {
		return false
	}

	return len(in.Status.Namespaces) >= int(*in.Spec.NamespaceOptions.Quota)
}

func (in *Tenant) AssignNamespaces(namespaces []corev1.Namespace) {
	l := make([]string, 0, len(namespaces))

	for _, ns := range namespaces {
		l = append(l, ns.GetName())
	}

	sort.Strings(l)

	in.Status.Namespaces = l
	in.Status.Size = uint(len(l))
}

func (in *Tenant) GetNamespaces() (res []string) {
	return in.Status.Namespaces
}

// Fetch all namespaces defined in the status.
func (in *Tenant) GetNamespaceObjects(ctx context.Context, c client.Reader) (namespaces []corev1.Namespace, err error) {
	nsList := &corev1.NamespaceList{}

	if len(in.Status.Namespaces) == 0 {
		return nsList.Items, nil
	}

	req, err := labels.NewRequirement(
		corev1.LabelMetadataName,
		selection.In,
		in.Status.Namespaces,
	)
	if err != nil {
		return nil, err
	}

	selector := labels.NewSelector().Add(*req)

	if err := c.List(ctx, nsList, client.MatchingLabelsSelector{Selector: selector}); err != nil {
		return nil, err
	}

	return nsList.Items, nil
}

func (in *Tenant) GetOwnerProxySettings(name string, kind rbac.OwnerKind) []rbac.ProxySettings {
	return in.Spec.Owners.FindOwner(name, kind).ProxyOperations
}

// GetClusterRolePermissions returns a map where the clusterRole is the key
// and the value is a list of permission subjects (kind and name) that reference that role.
// These mappings are gathered from the owners and additionalRolebindings spec.
func (in *Tenant) GetSubjectsByClusterRoles(ignoreOwnerKind []rbac.OwnerKind) (rolePerms map[string][]rbacv1.Subject) {
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
	for _, owner := range in.Status.Owners {
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

	return rolePerms
}

func (in *Tenant) GetClusterRolesBySubject(ignoreOwnerKind []rbac.OwnerKind) []rbac.SubjectRoles {
	ignore := make(map[string]struct{}, len(ignoreOwnerKind))
	for _, k := range ignoreOwnerKind {
		ignore[k.String()] = struct{}{}
	}

	roleSet := map[string]map[string]map[string]struct{}{}

	ensure := func(kind, name string) map[string]struct{} {
		km, ok := roleSet[kind]
		if !ok {
			km = map[string]map[string]struct{}{}
			roleSet[kind] = km
		}

		ns, ok := km[name]
		if !ok {
			ns = map[string]struct{}{}
			km[name] = ns
		}

		return ns
	}

	for _, owner := range in.Status.Owners {
		kind := owner.Kind.String()
		if _, skip := ignore[kind]; skip {
			continue
		}

		s := ensure(kind, owner.Name)
		for _, r := range owner.ClusterRoles {
			s[r] = struct{}{}
		}
	}

	for _, rb := range in.Spec.AdditionalRoleBindings {
		for _, subj := range rb.Subjects {
			if _, skip := ignore[subj.Kind]; skip {
				continue
			}

			s := ensure(subj.Kind, subj.Name)
			s[rb.ClusterRoleName] = struct{}{}
		}
	}

	// Flatten deterministically: sort kinds, names, roles
	kinds := make([]string, 0, len(roleSet))
	for k := range roleSet {
		kinds = append(kinds, k)
	}

	sort.Strings(kinds)

	totalSubjects := 0
	for _, byName := range roleSet {
		totalSubjects += len(byName)
	}

	out := make([]rbac.SubjectRoles, 0, totalSubjects)

	for _, kind := range kinds {
		names := make([]string, 0, len(roleSet[kind]))
		for n := range roleSet[kind] {
			names = append(names, n)
		}

		sort.Strings(names)

		for _, name := range names {
			roles := make([]string, 0, len(roleSet[kind][name]))
			for r := range roleSet[kind][name] {
				roles = append(roles, r)
			}

			sort.Strings(roles)

			out = append(out, rbac.SubjectRoles{Kind: kind, Name: name, Roles: roles})
		}
	}

	return out
}
