// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package template

// +kubebuilder:validation:Enum=default;zero;error
type MissingKeyOption string

func (p MissingKeyOption) String() string {
	return string(p)
}

const (
	MissingKeyDefault MissingKeyOption = "default"
	MissingKeyZero    MissingKeyOption = "zero"
	MissingKeyError   MissingKeyOption = "error"
)
