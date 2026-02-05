// Copyright 2020-2025 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package api

const (
	ResourceScopeNamespace ResourceScope = "Namespace"
	ResourceScopeTenant    ResourceScope = "Tenant"
	ResourceScopeCluster   ResourceScope = "Cluster"
)

// +kubebuilder:validation:Enum=Namespace;Tenant
type ResourceScope string

func (p ResourceScope) String() string {
	return string(p)
}
