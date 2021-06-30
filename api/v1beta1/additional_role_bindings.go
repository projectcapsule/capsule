// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package v1beta1

import rbacv1 "k8s.io/api/rbac/v1"

type AdditionalRoleBindingsSpec struct {
	ClusterRoleName string `json:"clusterRoleName"`
	// kubebuilder:validation:Minimum=1
	Subjects []rbacv1.Subject `json:"subjects"`
}
