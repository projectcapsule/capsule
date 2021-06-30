// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package v1beta1

// +kubebuilder:validation:Pattern="^([0-9]{1,3}.){3}[0-9]{1,3}(/([0-9]|[1-2][0-9]|3[0-2]))?$"
type AllowedIP string

type ExternalServiceIPsSpec struct {
	Allowed []AllowedIP `json:"allowed"`
}
