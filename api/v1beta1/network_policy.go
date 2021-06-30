// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package v1beta1

import (
	networkingv1 "k8s.io/api/networking/v1"
)

type NetworkPolicySpec struct {
	Items []networkingv1.NetworkPolicySpec `json:"items,omitempty"`
}
