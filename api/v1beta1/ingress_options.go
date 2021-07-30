// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package v1beta1

type IngressOptions struct {
	// Specifies the allowed IngressClasses assigned to the Tenant. Capsule assures that all Ingress resources created in the Tenant can use only one of the allowed IngressClasses. Optional.
	AllowedClasses *AllowedListSpec `json:"allowedClasses,omitempty"`
	// Specifies the allowed hostnames in Ingresses for the given Tenant. Capsule assures that all Ingress resources created in the Tenant can use only one of the allowed hostnames. Optional.
	AllowedHostnames *AllowedListSpec `json:"allowedHostnames,omitempty"`
}
