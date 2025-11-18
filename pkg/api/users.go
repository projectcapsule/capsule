// Copyright 2020-2025 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package api

// +kubebuilder:validation:Enum=User;Group;ServiceAccount
type UserKind string

func (k UserKind) String() string {
	return string(k)
}

// +kubebuilder:object:generate=true
type UserSpec struct {
	// Kind of entity. Possible values are "User", "Group", and "ServiceAccount"
	Kind OwnerKind `json:"kind"`
	// Name of the entity.
	Name string `json:"name"`
}
