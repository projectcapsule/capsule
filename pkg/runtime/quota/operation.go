// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package quota

// +kubebuilder:validation:Enum=add;sub;count
type Operation string

const (
	OpAdd   Operation = "add"
	OpSub   Operation = "sub"
	OpCount Operation = "count"
)
