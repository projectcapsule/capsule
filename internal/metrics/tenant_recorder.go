// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package metrics

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
	crtlmetrics "sigs.k8s.io/controller-runtime/pkg/metrics"
)

type TenantRecorder struct {
	ReconcilePhaseDuration           *prometheus.HistogramVec
	TenantNamespaceRelationshipGauge *prometheus.GaugeVec
	TenantNamespaceConditionGauge    *prometheus.GaugeVec
	TenantConditionGauge             *prometheus.GaugeVec
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
		ReconcilePhaseDuration: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Namespace: metricsPrefix,
				Name:      "tenant_reconcile_phase_duration_seconds",
				Help:      "Duration of Tenant reconciliation phases, including namespace cleanup.",
				Buckets:   []float64{0.001, 0.01, 0.1, 0.5, 1, 5, 10, 30, 60, 120},
			}, []string{"phase", "result"},
		),
		TenantNamespaceRelationshipGauge: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: metricsPrefix,
				Name:      "tenant_namespace_relationship",
				Help:      "Mapping metric showing namespace to tenant relationships",
			}, []string{"tenant", "target_namespace"},
		),
		TenantNamespaceConditionGauge: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: metricsPrefix,
				Name:      "tenant_namespace_condition",
				Help:      "Provides per namespace within a tenant condition status for each condition",
			}, []string{"tenant", "target_namespace", "condition"},
		),

		TenantConditionGauge: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: metricsPrefix,
				Name:      "tenant_condition",
				Help:      "Provides per tenant condition status for each condition",
			}, []string{"tenant", "condition"},
		),
		TenantNamespaceCounterGauge: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: metricsPrefix,
				Name:      "tenant_namespace_count",
				Help:      "Total number of namespaces currently owned by the tenant",
			}, []string{"tenant"},
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
		r.ReconcilePhaseDuration,
		r.TenantNamespaceRelationshipGauge,
		r.TenantNamespaceConditionGauge,
		r.TenantConditionGauge,
		r.TenantNamespaceCounterGauge,
		r.TenantResourceUsageGauge,
		r.TenantResourceLimitGauge,
	}
}

func (r *TenantRecorder) DeleteAllMetricsForNamespace(namespace string) {
	r.DeleteNamespaceRelationshipMetrics(namespace)
	r.DeleteTenantNamespaceConditionMetrics(namespace)
}

// DeleteCondition deletes the condition metrics for the ref.
func (r *TenantRecorder) DeleteNamespaceRelationshipMetrics(namespace string) {
	r.TenantNamespaceRelationshipGauge.DeletePartialMatch(map[string]string{
		"target_namespace": namespace,
	})
}

func (r *TenantRecorder) DeleteTenantNamespaceConditionMetrics(namespace string) {
	r.TenantNamespaceConditionGauge.DeletePartialMatch(map[string]string{
		"target_namespace": namespace,
	})
}

func (r *TenantRecorder) DeleteTenantNamespaceConditionMetricByType(namespace string, condition string) {
	r.TenantNamespaceConditionGauge.DeletePartialMatch(map[string]string{
		"target_namespace": namespace,
		"condition":        condition,
	})
}

func (r *TenantRecorder) DeleteAllMetricsForTenant(tenant string) {
	r.DeleteTenantResourceMetrics(tenant)
	r.DeleteTenantStatusMetrics(tenant)
	r.DeleteTenantConditionMetrics(tenant)
	r.DeleteTenantResourceMetrics(tenant)
}

func (r *TenantRecorder) DeleteTenantConditionMetrics(tenant string) {
	r.TenantConditionGauge.DeletePartialMatch(map[string]string{
		"tenant": tenant,
	})
}

func (r *TenantRecorder) DeleteTenantConditionMetricByType(tenant string, condition string) {
	r.TenantConditionGauge.DeletePartialMatch(map[string]string{
		"tenant":    tenant,
		"condition": condition,
	})
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
	r.TenantNamespaceCounterGauge.DeletePartialMatch(map[string]string{
		"tenant": tenant,
	})
	r.TenantNamespaceRelationshipGauge.DeletePartialMatch(map[string]string{
		"tenant": tenant,
	})
	r.TenantNamespaceConditionGauge.DeletePartialMatch(map[string]string{
		"tenant": tenant,
	})
}

func (r *TenantRecorder) ObserveReconcilePhase(phase string, duration time.Duration, err error) {
	result := "success"
	if err != nil {
		result = "error"
	}

	r.ReconcilePhaseDuration.WithLabelValues(phase, result).Observe(duration.Seconds())
}
