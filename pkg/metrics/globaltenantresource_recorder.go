// Copyright 2020-2023 Project Capsule Authors.
// SPDX-License-Identifier: Apache-2.0

package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	crtlmetrics "sigs.k8s.io/controller-runtime/pkg/metrics"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/meta"
)

type GlobalTenantResourceRecorder struct {
	resourceConditionGauge *prometheus.GaugeVec
}

func MustMakeGlobalTenantResourceRecorder() *GlobalTenantResourceRecorder {
	metricsRecorder := NewGlobalTenantResourceRecorder()
	crtlmetrics.Registry.MustRegister(metricsRecorder.Collectors()...)

	return metricsRecorder
}

func NewGlobalTenantResourceRecorder() *GlobalTenantResourceRecorder {
	return &GlobalTenantResourceRecorder{
		resourceConditionGauge: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: metricsPrefix,
				Name:      "global_resource_condition",
				Help:      "The current condition status of a global tenant resource.",
			},
			[]string{"name", "condition"},
		),
	}
}

func (r *GlobalTenantResourceRecorder) Collectors() []prometheus.Collector {
	return []prometheus.Collector{
		r.resourceConditionGauge,
	}
}

func (r *GlobalTenantResourceRecorder) RecordConditions(resource *capsulev1beta2.GlobalTenantResource) {
	for _, status := range []string{meta.ReadyCondition} {
		var value float64

		cond := resource.Status.Conditions.GetConditionByType(status)
		if cond == nil {
			r.DeleteConditionMetricByType(resource.GetName(), status)

			continue
		}

		if cond.Status == metav1.ConditionTrue {
			value = 1
		}

		r.resourceConditionGauge.WithLabelValues(resource.GetName(), resource.GetNamespace(), status).Set(value)
	}
}

func (r *GlobalTenantResourceRecorder) DeleteConditionMetrics(name string) {
	r.resourceConditionGauge.DeletePartialMatch(map[string]string{
		"name": name,
	})
}

func (r *GlobalTenantResourceRecorder) DeleteConditionMetricByType(name string, condition string) {
	r.resourceConditionGauge.DeletePartialMatch(map[string]string{
		"name":      name,
		"condition": condition,
	})
}

// DeleteCondition deletes the condition metrics for the ref.
func (r *GlobalTenantResourceRecorder) DeleteMetrics(resourceName string) {
	r.resourceConditionGauge.DeletePartialMatch(map[string]string{
		"name": resourceName,
	})

	r.DeleteConditionMetrics(resourceName)
}
