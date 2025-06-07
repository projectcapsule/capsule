// Copyright 2020-2023 Project Capsule Authors.
// SPDX-License-Identifier: Apache-2.0

package v1beta2

import (
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type TenantPermissionSpec struct {
	// Defines additional cluster-roles for the specific Owner.
	// +kubebuilder:default={admin,capsule-namespace-deleter}
	Bindings []string `json:"bindings,omitempty"`
	// kubebuilder:validation:Minimum=1
	Subjects []rbacv1.Subject `json:"subjects"`
	//+kubebuilder:default:=false
	ActAsOwner bool `json:"actAsOwner,omitempty"`
}

type TenantPermission struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              TenantPermissionSpec `json:"spec,omitempty"`
}
