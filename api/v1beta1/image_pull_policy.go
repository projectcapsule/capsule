// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package v1beta1

// +kubebuilder:validation:Enum=Always;Never;IfNotPresent
type ImagePullPolicySpec string

func (i ImagePullPolicySpec) String() string {
	return string(i)
}
