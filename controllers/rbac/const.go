// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package rbac

import (
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	ProvisionerRoleName = "capsule-namespace-provisioner"
	DeleterRoleName     = "capsule-namespace-deleter"
)

var (
	clusterRoles = map[string]*rbacv1.ClusterRole{
		ProvisionerRoleName: {
			ObjectMeta: metav1.ObjectMeta{
				Name: ProvisionerRoleName,
			},
			Rules: []rbacv1.PolicyRule{
				{
					APIGroups: []string{""},
					Resources: []string{"namespaces"},
					Verbs:     []string{"create"},
				},
			},
		},
		DeleterRoleName: {
			ObjectMeta: metav1.ObjectMeta{
				Name: DeleterRoleName,
			},
			Rules: []rbacv1.PolicyRule{
				{
					APIGroups: []string{""},
					Resources: []string{"namespaces"},
					Verbs:     []string{"delete", "patch"},
				},
			},
		},
	}

	provisionerClusterRoleBinding = &rbacv1.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name: ProvisionerRoleName,
		},
		RoleRef: rbacv1.RoleRef{
			Kind:     "ClusterRole",
			Name:     ProvisionerRoleName,
			APIGroup: rbacv1.GroupName,
		},
	}
)
