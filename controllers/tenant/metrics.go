// Copyright 2020-2025 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0
package tenant

import capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"

// Exposing Status Metrics for tenant.
func (r *Manager) syncStatusMetrics(tenant *capsulev1beta2.Tenant) {
	var cordoned float64 = 0
	// Reset all metrics
	r.Metrics.DeleteTenantStatusMetrics(tenant.GetName())
	// Expose namespace-tenant relationship
	for _, ns := range tenant.Status.Namespaces {
		r.Metrics.TenantNamespaceRelationshipGauge.WithLabelValues(tenant.GetName(), ns).Set(1)
	}

	if tenant.Spec.Cordoned {
		cordoned = 1
	}
	// Expose cordoned status
	r.Metrics.TenantNamespaceCounterGauge.WithLabelValues(tenant.Name, "namespaces").Set(float64(tenant.Status.Size))
	// Expose the namespace counter
	r.Metrics.TenantCordonedStatusGauge.WithLabelValues(tenant.Name).Set(cordoned)
}
