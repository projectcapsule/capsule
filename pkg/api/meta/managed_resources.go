// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package meta

// ManagedResourcesStatus is the common status of an API object which manages
// Kubernetes resources.
// +kubebuilder:object:generate=true
type ManagedResourcesStatus struct {
	// List of resources managed by the API object.
	// +optional
	ProcessedItems ProcessedItems `json:"processedItems,omitzero"`

	// Number of managed resources.
	Size uint `json:"size"`
}

// UpdateStats refreshes the derived managed-resource counters.
func (s *ManagedResourcesStatus) UpdateStats() {
	s.Size = uint(len(s.ProcessedItems))
}
