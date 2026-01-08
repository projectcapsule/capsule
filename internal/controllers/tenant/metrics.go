// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package tenant

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/meta"
)

// Exposing Status Metrics for tenant.
func (r *Manager) syncTenantStatusMetrics(tenant *capsulev1beta2.Tenant) {
	// Expose namespace-tenant relationship
	for _, ns := range tenant.Status.Namespaces {
		r.Metrics.TenantNamespaceRelationshipGauge.WithLabelValues(tenant.GetName(), ns).Set(1)
	}

	// Expose cordoned status
	r.Metrics.TenantNamespaceCounterGauge.WithLabelValues(tenant.Name).Set(float64(tenant.Status.Size))

	// Expose Status Metrics
	for _, status := range []string{meta.ReadyCondition, meta.CordonedCondition} {
		var value float64

		cond := tenant.Status.Conditions.GetConditionByType(status)
		if cond == nil {
			r.Metrics.DeleteTenantConditionMetricByType(tenant.Name, status)

			continue
		}

		if cond.Status == metav1.ConditionTrue {
			value = 1
		}

		r.Metrics.TenantConditionGauge.WithLabelValues(tenant.GetName(), status).Set(value)
	}
}

// Exposing Status Metrics for tenant.
func (r *Manager) syncNamespaceStatusMetrics(tenant *capsulev1beta2.Tenant, namespace *corev1.Namespace) {
	for _, status := range []string{meta.ReadyCondition, meta.CordonedCondition} {
		var value float64

		cond := tenant.Status.Conditions.GetConditionByType(status)
		if cond == nil {
			r.Metrics.DeleteTenantNamespaceConditionMetricByType(namespace.Name, status)

			continue
		}

		if cond.Status == metav1.ConditionTrue {
			value = 1
		}

		r.Metrics.TenantNamespaceConditionGauge.WithLabelValues(tenant.GetName(), namespace.GetName(), status).Set(value)
	}
}
