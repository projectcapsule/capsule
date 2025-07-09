// Copyright 2020-2025 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	crtlmetrics "sigs.k8s.io/controller-runtime/pkg/metrics"
)

type TenantRecorder struct {
	TenantNamespaceRelationshipGauge *prometheus.GaugeVec
	TenantCordonedStatusGauge        *prometheus.GaugeVec
	TenantNamespaceCounterGauge      *prometheus.GaugeVec
	TenantResourceUsageGauge         *prometheus.GaugeVec
	TenantResourceLimitGauge         *prometheus.GaugeVec
}

func MustMakeTenantRecorder() *TenantRecorder {
	metricsRecorder := NewTenantRecorder()
	crtlmetrics.Registry.MustRegister(metricsRecorder.Collectors()...)

	return metricsRecorder
}

func NewTenantRecorder() *TenantRecorder {
	return &TenantRecorder{
		TenantNamespaceRelationshipGauge: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: metricsPrefix,
				Name:      "tenant_namespace_relationship",
				Help:      "Current cordon status of tenant",
			}, []string{"tenant", "namespace"},
		),
		TenantCordonedStatusGauge: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: metricsPrefix,
				Name:      "tenant_status",
				Help:      "Current cordon status of tenant",
			}, []string{"tenant"},
		),
		TenantNamespaceCounterGauge: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: metricsPrefix,
				Name:      "tenant_namespace_count",
				Help:      "Current count of tenant namespaces",
			}, []string{"tenant", "resource"},
		),
		TenantResourceUsageGauge: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: metricsPrefix,
				Name:      "tenant_resource_usage",
				Help:      "Current resource usage for a given resource in a tenant",
			}, []string{"tenant", "resource", "resourcequotaindex"},
		),
		TenantResourceLimitGauge: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: metricsPrefix,
				Name:      "tenant_resource_limit",
				Help:      "Current resource limit for a given resource in a tenant",
			}, []string{"tenant", "resource", "resourcequotaindex"},
		),
	}
}

func (r *TenantRecorder) Collectors() []prometheus.Collector {
	return []prometheus.Collector{
		r.TenantNamespaceRelationshipGauge,
		r.TenantCordonedStatusGauge,
		r.TenantNamespaceCounterGauge,
		r.TenantResourceUsageGauge,
		r.TenantResourceLimitGauge,
	}
}

// DeleteCondition deletes the condition metrics for the ref.
func (r *TenantRecorder) DeleteTenantResourceMetrics(tenant string) {
	r.TenantResourceUsageGauge.DeletePartialMatch(map[string]string{
		"tenant": tenant,
	})
	r.TenantResourceLimitGauge.DeletePartialMatch(map[string]string{
		"tenant": tenant,
	})
}

// DeleteCondition deletes the condition metrics for the ref.
func (r *TenantRecorder) DeleteTenantStatusMetrics(tenant string) {
	r.TenantNamespaceRelationshipGauge.DeletePartialMatch(map[string]string{
		"tenant": tenant,
	})
	r.TenantResourceUsageGauge.DeletePartialMatch(map[string]string{
		"tenant": tenant,
	})
	r.TenantResourceLimitGauge.DeletePartialMatch(map[string]string{
		"tenant": tenant,
	})
}

func (r *TenantRecorder) DeleteAllMetrics(tenant string) {
	r.DeleteTenantResourceMetrics(tenant)
	r.DeleteTenantStatusMetrics(tenant)
}
