// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package api

import corev1 "k8s.io/api/core/v1"

// +kubebuilder:validation:Enum=Tenant;Namespace
type ResourceQuotaScope string

const (
	ResourceQuotaScopeTenant    ResourceQuotaScope = "Tenant"
	ResourceQuotaScopeNamespace ResourceQuotaScope = "Namespace"
)

// +kubebuilder:object:generate=true

type ResourceQuotaSpec struct {
	// +kubebuilder:default=Tenant
	// Define if the Resource Budget should compute resource across all Namespaces in the Tenant or individually per cluster. Default is Tenant
	Scope ResourceQuotaScope         `json:"scope,omitempty"`
	Items []corev1.ResourceQuotaSpec `json:"items,omitempty"`
}
