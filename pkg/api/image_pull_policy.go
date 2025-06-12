// Copyright 2020-2025 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package api

// +kubebuilder:validation:Enum=Always;Never;IfNotPresent
type ImagePullPolicySpec string

func (i ImagePullPolicySpec) String() string {
	return string(i)
}
