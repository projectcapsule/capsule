// Copyright 2020-2025 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package utils

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/runtime"

	"github.com/projectcapsule/capsule/api/v1beta1"
	"github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/meta"
)

func GetTypeLabel(t runtime.Object) (label string, err error) {
	switch v := t.(type) {
	case *v1beta1.Tenant, *v1beta2.Tenant:
		return meta.TenantLabel, nil
	case *v1beta2.ResourcePool:
		return meta.ResourcePoolLabel, nil
	case *corev1.LimitRange:
		return meta.LimitRangeLabel, nil
	case *networkingv1.NetworkPolicy:
		return meta.NetworkPolicyLabel, nil
	case *corev1.ResourceQuota:
		return meta.ResourceQuotaLabel, nil
	case *rbacv1.RoleBinding:
		return meta.RolebindingLabel, nil
	default:
		err = fmt.Errorf("type %T is not mapped as Capsule label recognized", v)
	}

	return label, err
}
