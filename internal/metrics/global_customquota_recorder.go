// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	crtlmetrics "sigs.k8s.io/controller-runtime/pkg/metrics"
)

type GlobalCustomQuotaRecorder struct {
	ConditionGauge         *prometheus.GaugeVec
	ResourceUsageGauge     *prometheus.GaugeVec
	ResourceLimitGauge     *prometheus.GaugeVec
	ResourceAvailableGauge *prometheus.GaugeVec
	ResourceItemUsageGauge *prometheus.GaugeVec
}

func MustMakeGlobalCustomQuotaRecorder() *GlobalCustomQuotaRecorder {
	metricsRecorder := NewGlobalCustomQuotaRecorder()
	crtlmetrics.Registry.MustRegister(metricsRecorder.Collectors()...)

	return metricsRecorder
}

func NewGlobalCustomQuotaRecorder() *GlobalCustomQuotaRecorder {
	return &GlobalCustomQuotaRecorder{
		ConditionGauge: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: metricsPrefix,
				Name:      "global_custom_quota_condition",
				Help:      "Provides per global custom quota condition status",
			}, []string{"custom_quota", "condition"},
		),
		ResourceUsageGauge: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: metricsPrefix,
				Name:      "global_custom_quota_resource_usage",
				Help:      "Current resource usage for given global custom quota",
			}, []string{"custom_quota"},
		),
		ResourceLimitGauge: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: metricsPrefix,
				Name:      "global_custom_quota_resource_limit",
				Help:      "Current resource limit for given global custom quota",
			}, []string{"custom_quota"},
		),
		ResourceAvailableGauge: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: metricsPrefix,
				Name:      "global_custom_quota_resource_available",
				Help:      "Available resources for given global_custom quota",
			}, []string{"custom_quota"},
		),
		ResourceItemUsageGauge: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: metricsPrefix,
				Name:      "global_custom_quota_resource_item_usage",
				Help:      "Claimed resources from given item",
			}, []string{"custom_quota", "name", "target_namespace", "kind", "group"},
		),
	}
}

func (r *GlobalCustomQuotaRecorder) Collectors() []prometheus.Collector {
	return []prometheus.Collector{
		r.ConditionGauge,
		r.ResourceUsageGauge,
		r.ResourceLimitGauge,
		r.ResourceAvailableGauge,
		r.ResourceItemUsageGauge,
	}
}

func (r *GlobalCustomQuotaRecorder) DeleteAllMetricsForGlobalCustomQuota(name string) {
	r.ConditionGauge.DeletePartialMatch(map[string]string{
		"custom_quota": name,
	})
	r.ResourceUsageGauge.DeletePartialMatch(map[string]string{
		"custom_quota": name,
	})
	r.ResourceLimitGauge.DeletePartialMatch(map[string]string{
		"custom_quota": name,
	})
	r.ResourceAvailableGauge.DeletePartialMatch(map[string]string{
		"custom_quota": name,
	})
	r.ResourceItemUsageGauge.DeletePartialMatch(map[string]string{
		"custom_quota": name,
	})

}

func (r *GlobalCustomQuotaRecorder) DeleteConditionMetricByType(name string, condition string) {
	r.ConditionGauge.DeletePartialMatch(map[string]string{
		"custom_quota": name,
		"condition":    condition,
	})
}
