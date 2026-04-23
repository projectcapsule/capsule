// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package v1beta2

import "slices"

// CustomQuotaStatus defines the observed state of GlobalResourceQuota.
type GlobalCustomQuotaStatus struct {
	CustomQuotaStatus `json:",inline"`

	// Observed Namespaces
	Namespaces []string `json:"namespaces,omitempty"`
}

func (g *GlobalCustomQuotaStatus) NamespacePresent(ns string) bool {
	return slices.Contains(g.Namespaces, ns)
}
