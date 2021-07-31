// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package v1beta1

const (
	HostnameCollisionScopeCluster   HostnameCollisionScope = "Cluster"
	HostnameCollisionScopeTenant    HostnameCollisionScope = "Tenant"
	HostnameCollisionScopeNamespace HostnameCollisionScope = "Namespace"
	HostnameCollisionScopeDisabled  HostnameCollisionScope = "Disabled"
)

// +kubebuilder:validation:Enum=Cluster;Tenant;Namespace;Disabled
type HostnameCollisionScope string
