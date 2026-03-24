// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	crtlmetrics "sigs.k8s.io/controller-runtime/pkg/metrics"
)

type CustomQuotaRecorder struct {
	ConditionGauge         *prometheus.GaugeVec
	ResourceUsageGauge     *prometheus.GaugeVec
	ResourceLimitGauge     *prometheus.GaugeVec
	ResourceAvailableGauge *prometheus.GaugeVec
}

func MustMakeCustomQuotaRecorder() *CustomQuotaRecorder {
	metricsRecorder := NewCustomQuotaRecorder()
	crtlmetrics.Registry.MustRegister(metricsRecorder.Collectors()...)

	return metricsRecorder
}

func NewCustomQuotaRecorder() *CustomQuotaRecorder {
	return &CustomQuotaRecorder{
		ConditionGauge: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: metricsPrefix,
				Name:      "custom_quota_condition",
				Help:      "Provides per custom quota condition status",
			}, []string{"custom_quota", "target_namespace", "condition"},
		),
		ResourceUsageGauge: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: metricsPrefix,
				Name:      "custom_quota_resource_usage",
				Help:      "Current resource usage for given custom quota",
			}, []string{"custom_quota", "target_namespace"},
		),
		ResourceLimitGauge: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: metricsPrefix,
				Name:      "custom_quota_resource_limit",
				Help:      "Current resource limit for given custom quota",
			}, []string{"custom_quota", "target_namespace"},
		),
		ResourceAvailableGauge: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: metricsPrefix,
				Name:      "custom_quota_resource_available",
				Help:      "Available resources for given custom quota",
			}, []string{"custom_quota", "target_namespace"},
		),
	}
}

func (r *CustomQuotaRecorder) Collectors() []prometheus.Collector {
	return []prometheus.Collector{
		r.ConditionGauge,
		r.ResourceUsageGauge,
		r.ResourceLimitGauge,
		r.ResourceAvailableGauge,
	}
}

func (r *CustomQuotaRecorder) DeleteAllMetricsForCustomQuota(name string, namespace string) {
	r.ConditionGauge.DeletePartialMatch(map[string]string{
		"custom_quota":     name,
		"target_namespace": namespace,
	})
	r.ResourceUsageGauge.DeletePartialMatch(map[string]string{
		"custom_quota":     name,
		"target_namespace": namespace,
	})
	r.ResourceLimitGauge.DeletePartialMatch(map[string]string{
		"custom_quota":     name,
		"target_namespace": namespace,
	})
	r.ResourceAvailableGauge.DeletePartialMatch(map[string]string{
		"custom_quota":     name,
		"target_namespace": namespace,
	})
}

func (r *CustomQuotaRecorder) DeleteConditionMetricByType(name string, namespace string, condition string) {
	r.ConditionGauge.DeletePartialMatch(map[string]string{
		"custom_quota":     name,
		"target_namespace": namespace,
		"condition":        condition,
	})
}
