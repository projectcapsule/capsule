// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	crtlmetrics "sigs.k8s.io/controller-runtime/pkg/metrics"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/meta"
)

type GlobalResourceQuotaRecorder struct {
	ConditionGauge                *prometheus.GaugeVec
	ResourceLimitGauge            *prometheus.GaugeVec
	ResourceUsageGauge            *prometheus.GaugeVec
	ResourceAvailableGauge        *prometheus.GaugeVec
	ResourceUsagePercentageGauge  *prometheus.GaugeVec
	NamespaceUsageGauge           *prometheus.GaugeVec
	NamespaceUsagePercentageGauge *prometheus.GaugeVec
}

func MustMakeGlobalResourceQuotaRecorder() *GlobalResourceQuotaRecorder {
	recorder := NewGlobalResourceQuotaRecorder()
	crtlmetrics.Registry.MustRegister(recorder.Collectors()...)

	return recorder
}

func NewGlobalResourceQuotaRecorder() *GlobalResourceQuotaRecorder {
	const label = "global_resource_quota"

	return &GlobalResourceQuotaRecorder{
		ConditionGauge: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: metricsPrefix,
			Name:      "global_resource_quota_condition",
			Help:      "Current condition for a GlobalResourceQuota.",
		}, []string{label, "condition"}),
		ResourceLimitGauge: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: metricsPrefix,
			Name:      "global_resource_quota_limit",
			Help:      "Shared hard limit for a GlobalResourceQuota resource.",
		}, []string{label, "resource"}),
		ResourceUsageGauge: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: metricsPrefix,
			Name:      "global_resource_quota_usage",
			Help:      "Observed aggregate usage for a GlobalResourceQuota resource.",
		}, []string{label, "resource"}),
		ResourceAvailableGauge: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: metricsPrefix,
			Name:      "global_resource_quota_available",
			Help:      "Available aggregate capacity for a GlobalResourceQuota resource.",
		}, []string{label, "resource"}),
		ResourceUsagePercentageGauge: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: metricsPrefix,
			Name:      "global_resource_quota_usage_percentage",
			Help:      "Observed aggregate usage percentage for a GlobalResourceQuota resource.",
		}, []string{label, "resource"}),
		NamespaceUsageGauge: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: metricsPrefix,
			Name:      "global_resource_quota_namespace_usage",
			Help:      "Observed usage per namespace for a GlobalResourceQuota resource.",
		}, []string{label, "target_namespace", "resource"}),
		NamespaceUsagePercentageGauge: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: metricsPrefix,
			Name:      "global_resource_quota_namespace_usage_percentage",
			Help:      "Observed per-namespace usage as a percentage of the shared limit.",
		}, []string{label, "target_namespace", "resource"}),
	}
}

func (r *GlobalResourceQuotaRecorder) Collectors() []prometheus.Collector {
	return []prometheus.Collector{
		r.ConditionGauge,
		r.ResourceLimitGauge,
		r.ResourceUsageGauge,
		r.ResourceAvailableGauge,
		r.ResourceUsagePercentageGauge,
		r.NamespaceUsageGauge,
		r.NamespaceUsagePercentageGauge,
	}
}

func (r *GlobalResourceQuotaRecorder) Record(quota *capsulev1beta2.GlobalResourceQuota) {
	r.Delete(quota.Name)

	for _, conditionType := range []string{meta.ReadyCondition} {
		condition := quota.Status.Conditions.GetConditionByType(conditionType)
		if condition == nil {
			continue
		}

		value := float64(0)
		if condition.Status == metav1.ConditionTrue {
			value = 1
		}

		r.ConditionGauge.WithLabelValues(quota.Name, conditionType).Set(value)
	}

	for name, hard := range quota.Status.Total.Hard {
		used := quota.Status.Total.Used[name]
		available := quota.Status.Total.Available[name]

		r.ResourceLimitGauge.WithLabelValues(quota.Name, name.String()).Set(quantityMetric(hard))
		r.ResourceUsageGauge.WithLabelValues(quota.Name, name.String()).Set(quantityMetric(used))
		r.ResourceAvailableGauge.WithLabelValues(quota.Name, name.String()).Set(quantityMetric(available))
		r.ResourceUsagePercentageGauge.WithLabelValues(quota.Name, name.String()).Set(quantityPercentage(used, hard))
	}

	for namespace, usage := range quota.Status.NamespaceUsage {
		for name, used := range usage.Used {
			hard := quota.Status.Total.Hard[name]
			r.NamespaceUsageGauge.WithLabelValues(quota.Name, namespace, name.String()).Set(quantityMetric(used))
			r.NamespaceUsagePercentageGauge.WithLabelValues(
				quota.Name,
				namespace,
				name.String(),
			).Set(quantityPercentage(used, hard))
		}
	}
}

func (r *GlobalResourceQuotaRecorder) Delete(name string) {
	labels := prometheus.Labels{"global_resource_quota": name}
	r.ConditionGauge.DeletePartialMatch(labels)
	r.ResourceLimitGauge.DeletePartialMatch(labels)
	r.ResourceUsageGauge.DeletePartialMatch(labels)
	r.ResourceAvailableGauge.DeletePartialMatch(labels)
	r.ResourceUsagePercentageGauge.DeletePartialMatch(labels)
	r.NamespaceUsageGauge.DeletePartialMatch(labels)
	r.NamespaceUsagePercentageGauge.DeletePartialMatch(labels)
}

func quantityMetric(quantity resource.Quantity) float64 {
	return float64(quantity.MilliValue()) / 1000
}

func quantityPercentage(used, hard resource.Quantity) float64 {
	if hard.MilliValue() <= 0 {
		return 0
	}

	return float64(used.MilliValue()) / float64(hard.MilliValue()) * 100
}
