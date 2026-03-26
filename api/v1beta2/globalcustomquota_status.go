// Copyright 2020-2025 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package v1beta2

// CustomQuotaStatus defines the observed state of GlobalResourceQuota.
type GlobalCustomQuotaStatus struct {
	CustomQuotaStatus `json:",inline"`

	// Observed Namespaces
	Namespaces []string `json:"namespaces,omitempty"`
}

func (g *GlobalCustomQuotaStatus) NamespacePresent(ns string) bool {
	for _, n := range g.Namespaces {
		if n == ns {
			return true
		}
	}

	return false
}
