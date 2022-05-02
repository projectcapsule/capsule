// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package v1alpha1

// OwnerSpec defines tenant owner name and kind.
type OwnerSpec struct {
	Name string `json:"name"`
	Kind Kind   `json:"kind"`
}

// +kubebuilder:validation:Enum=User;Group
type Kind string

func (k Kind) String() string {
	return string(k)
}
