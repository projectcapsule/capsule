// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package rbac

import (
	rbacv1 "k8s.io/api/rbac/v1"

	"github.com/projectcapsule/capsule/pkg/api/meta"
)

// +kubebuilder:object:generate=true
type PromotionSpec struct {
	UserSpec `json:",inline"`

	// Defines additional cluster-roles for the specific Owner.
	// +kubebuilder:default={admin,capsule-namespace-deleter}
	ClusterRoles []string `json:"clusterRoles,omitempty"`

	// Defines additional cluster-roles for the specific Owner.
	Targets []string `json:"targets,omitempty"`
}

func (o PromotionSpec) ToAdditionalRolebindings() []AdditionalRoleBindingsWithNamespaceSpec {
	bindings := make([]AdditionalRoleBindingsWithNamespaceSpec, 0, len(o.ClusterRoles))

	for _, ns := range o.Targets {
		for _, clusterRoleName := range o.ClusterRoles {
			bindings = append(bindings, AdditionalRoleBindingsWithNamespaceSpec{
				Namespace: meta.RFC1123SubdomainName(ns),
				AdditionalRoleBindingsSpec: AdditionalRoleBindingsSpec{
					ClusterRoleName: clusterRoleName,
					Subjects: []rbacv1.Subject{
						o.Subject(),
					},
				},
			})
		}
	}

	return bindings
}
