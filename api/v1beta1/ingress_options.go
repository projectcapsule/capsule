// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package v1beta1

import (
	"github.com/clastix/capsule/pkg/api"
)

type IngressOptions struct {
	// Specifies the allowed IngressClasses assigned to the Tenant. Capsule assures that all Ingress resources created in the Tenant can use only one of the allowed IngressClasses. Optional.
	AllowedClasses *api.AllowedListSpec `json:"allowedClasses,omitempty"`
	// Defines the scope of hostname collision check performed when Tenant Owners create Ingress with allowed hostnames.
	//
	//
	// - Cluster: disallow the creation of an Ingress if the pair hostname and path is already used across the Namespaces managed by Capsule.
	//
	// - Tenant: disallow the creation of an Ingress if the pair hostname and path is already used across the Namespaces of the Tenant.
	//
	// - Namespace: disallow the creation of an Ingress if the pair hostname and path is already used in the Ingress Namespace.
	//
	//
	// Optional.
	// +kubebuilder:default=Disabled
	HostnameCollisionScope api.HostnameCollisionScope `json:"hostnameCollisionScope,omitempty"`
	// Specifies the allowed hostnames in Ingresses for the given Tenant. Capsule assures that all Ingress resources created in the Tenant can use only one of the allowed hostnames. Optional.
	AllowedHostnames *api.AllowedListSpec `json:"allowedHostnames,omitempty"`
}
