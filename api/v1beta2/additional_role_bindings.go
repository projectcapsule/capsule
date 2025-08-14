// Copyright 2020-2025 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package v1beta2

import rbacv1 "k8s.io/api/rbac/v1"

type AdditionalRoleBindingsSpec struct {
	ClusterRoleName string `json:"clusterRoleName"`
	// kubebuilder:validation:Minimum=1
	Subjects []rbacv1.Subject `json:"subjects"`
}
