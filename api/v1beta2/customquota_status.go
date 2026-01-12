// Copyright 2020-2025 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package v1beta2

import "k8s.io/apimachinery/pkg/api/resource"

// CustomQuotaStatus defines the observed state of GlobalResourceQuota.
type CustomQuotaStatus struct {
	Used      resource.Quantity `json:"used"`
	Available resource.Quantity `json:"available"`
	Claims    []string          `json:"claims,omitempty"`
}
