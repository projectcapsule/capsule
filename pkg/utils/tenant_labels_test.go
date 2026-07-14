// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package utils_test

import (
	"testing"

	capsulev1beta1 "github.com/projectcapsule/capsule/api/v1beta1"
	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/utils"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

func TestGetTypeLabel(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		obj  runtime.Object
		want string
	}{
		{name: "v1beta1 tenant", obj: &capsulev1beta1.Tenant{}, want: meta.TenantLabel},
		{name: "v1beta2 tenant", obj: &capsulev1beta2.Tenant{}, want: meta.TenantLabel},
		{name: "resource pool", obj: &capsulev1beta2.ResourcePool{}, want: meta.ResourcePoolLabel},
		{name: "limit range", obj: &corev1.LimitRange{}, want: meta.LimitRangeLabel},
		{name: "network policy", obj: &networkingv1.NetworkPolicy{}, want: meta.NetworkPolicyLabel},
		{name: "resource quota", obj: &corev1.ResourceQuota{}, want: meta.ResourceQuotaLabel},
		{name: "role binding", obj: &rbacv1.RoleBinding{}, want: meta.RolebindingLabel},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := utils.GetTypeLabel(tt.obj)
			if err != nil {
				t.Fatalf("GetTypeLabel() unexpected error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("GetTypeLabel() = %q, want %q", got, tt.want)
			}
		})
	}

	if _, err := utils.GetTypeLabel(&corev1.Pod{}); err == nil {
		t.Fatalf("GetTypeLabel() expected error for unmapped type")
	}
}
