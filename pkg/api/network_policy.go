// Copyright 2020-2025 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package api

import (
	networkingv1 "k8s.io/api/networking/v1"
)

// +kubebuilder:object:generate=true

type NetworkPolicySpec struct {
	Items []networkingv1.NetworkPolicySpec `json:"items,omitempty"`
}
