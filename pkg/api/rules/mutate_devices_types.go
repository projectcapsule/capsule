// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package rules

// DeviceMutation supplies DeviceClass defaults on ResourceClaim and
// ResourceClaimTemplate creation. It never replaces an explicit class name.
// +kubebuilder:object:generate=true
type DeviceMutation struct {
	// Class fills an omitted, null, or empty deviceClassName in each existing
	// exactly request and firstAvailable alternative. Explicit names and request
	// structure are preserved. The first applicable default wins. Supports merge
	// only; replace is rejected to preserve scheduler-selected allocations.
	// Omitted or null supplies no default. Tenant and Namespace templates are
	// supported; rendered names must satisfy native DeviceClass name validation.
	// +optional
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=4096
	Class string `json:"class,omitempty"`
}
