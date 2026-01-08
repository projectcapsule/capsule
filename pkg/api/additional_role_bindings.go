// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package api

import rbacv1 "k8s.io/api/rbac/v1"

// +kubebuilder:object:generate=true

type AdditionalRoleBindingsSpec struct {
	ClusterRoleName string `json:"clusterRoleName"`
	// kubebuilder:validation:Minimum=1
	Subjects []rbacv1.Subject `json:"subjects"`
	// Additional Labels for the synchronized rolebindings
	Labels map[string]string `json:"labels,omitempty"`
	// Additional Annotations for the synchronized rolebindings
	Annotations map[string]string `json:"annotations,omitempty"`
}
