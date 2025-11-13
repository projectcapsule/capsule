// Copyright 2020-2025 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package api

// +kubebuilder:object:generate=true

type UserSpec struct {
	// Kind of tenant owner. Possible values are "User", "Group", and "ServiceAccount"
	Kind OwnerKind `json:"kind"`
	// Name of tenant owner.
	Name string `json:"name"`
}
