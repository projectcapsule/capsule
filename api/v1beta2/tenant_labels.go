// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package v1beta2

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func GetTypeLabel(t metav1.Object) (label string, err error) {
	switch v := t.(type) {
	case *Tenant:
		return "capsule.clastix.io/tenant", nil
	case *corev1.LimitRange:
		return "capsule.clastix.io/limit-range", nil
	case *networkingv1.NetworkPolicy:
		return "capsule.clastix.io/network-policy", nil
	case *corev1.ResourceQuota:
		return "capsule.clastix.io/resource-quota", nil
	case *rbacv1.RoleBinding:
		return "capsule.clastix.io/role-binding", nil
	default:
		err = fmt.Errorf("type %T is not mapped as Capsule label recognized", v)
	}

	return
}
