// Copyright 2020-2023 Project Capsule Authors.
// SPDX-License-Identifier: Apache-2.0

package v1beta2

import (
	rbacv1 "k8s.io/api/rbac/v1"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type ExtendedSubject struct {
	rbacv1.Subject
	// `json:"actAsOwner,omitempty"
	//  +kubebuilder:default=false`
	ActAsOwner bool `json:"actAsOwner,omitempty"`
}

type PermissionSpec struct {
	// Defines roleBindings between the ClusterRole and the Subjet in the Tenants namespaces.
	// +kubebuilder:default={admin,capsule-namespace-deleter}
	RoleBindings []string `json:"roleBindings,omitempty"`
	// Defines additional cluster-role-bindings the specific subject should be assigned.
	ClusterRoleBindings []string `json:"clusterRoleBindings,omitempty"`
	// kubebuilder:validation:Minimum=1
	Subjects          []ExtendedSubject     `json:"subjects"`
	NamespaceSelector *metav1.LabelSelector `json:"namespaceSelector,omitempty"`
}
