// Copyright 2020-2025 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0
package tenant

import (
	"slices"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
)

// Exposing Status Metrics for tenant.
func (r *Manager) syncStatusMetrics(tenant *capsulev1beta2.Tenant, preRecNamespaces []string) {
	var cordoned float64 = 0

	// Expose namespace-tenant relationship
	for _, ns := range tenant.Status.Namespaces {
		r.Metrics.TenantNamespaceRelationshipGauge.WithLabelValues(tenant.GetName(), ns).Set(1)
	}

	// Cleanup deleted namespaces
	for _, ns := range preRecNamespaces {
		if !slices.Contains(tenant.Status.Namespaces, ns) {
			r.Metrics.DeleteNamespaceRelationshipMetrics(ns)
		}
	}

	if tenant.Spec.Cordoned {
		cordoned = 1
	}
	// Expose cordoned status
	r.Metrics.TenantNamespaceCounterGauge.WithLabelValues(tenant.Name, "namespaces").Set(float64(tenant.Status.Size))
	// Expose the namespace counter
	r.Metrics.TenantCordonedStatusGauge.WithLabelValues(tenant.Name).Set(cordoned)
}
