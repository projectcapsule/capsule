// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package template

// +kubebuilder:validation:Enum=invalid;zero;error
type MissingKeyOption string

func (p MissingKeyOption) String() string {
	return string(p)
}

const (
	MissingKeyInvalid MissingKeyOption = "invalid"
	MissingKeyZero    MissingKeyOption = "zero"
	MissingKeyError   MissingKeyOption = "error"
)
