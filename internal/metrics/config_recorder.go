// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

//nolint:dupl
package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	crtlmetrics "sigs.k8s.io/controller-runtime/pkg/metrics"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/meta"
)

type ConfigRecorder struct {
	resourceConditionGauge *prometheus.GaugeVec
}

func MustMakeConfigRecorder() *ConfigRecorder {
	metricsRecorder := NewConfigRecorder()
	crtlmetrics.Registry.MustRegister(metricsRecorder.Collectors()...)

	return metricsRecorder
}

func NewConfigRecorder() *ConfigRecorder {
	return &ConfigRecorder{
		resourceConditionGauge: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: metricsPrefix,
				Name:      "config_condition",
				Help:      "The current condition status of a config resource.",
			},
			[]string{"name", "condition"},
		),
	}
}

func (r *ConfigRecorder) Collectors() []prometheus.Collector {
	return []prometheus.Collector{
		r.resourceConditionGauge,
	}
}

// RecordCondition records the condition as given for the ref.
func (r *ConfigRecorder) RecordConditions(resource *capsulev1beta2.CapsuleConfiguration) {
	for _, status := range []string{meta.ReadyCondition, meta.CordonedCondition} {
		var value float64

		cond := resource.Status.Conditions.GetConditionByType(status)
		if cond == nil {
			r.DeleteConditionMetricByType(resource.GetName(), status)

			continue
		}

		if cond.Status == metav1.ConditionTrue {
			value = 1
		}

		r.resourceConditionGauge.WithLabelValues(resource.GetName(), status).Set(value)
	}
}

func (r *ConfigRecorder) DeleteConditionMetrics(name string) {
	r.resourceConditionGauge.DeletePartialMatch(map[string]string{
		"name": name,
	})
}

func (r *ConfigRecorder) DeleteConditionMetricByType(name string, condition string) {
	r.resourceConditionGauge.DeletePartialMatch(map[string]string{
		"name":      name,
		"condition": condition,
	})
}

// DeleteCondition deletes the condition metrics for the ref.
func (r *ConfigRecorder) DeleteMetrics(resourceName string) {
	r.resourceConditionGauge.DeletePartialMatch(map[string]string{
		"name": resourceName,
	})

	r.DeleteConditionMetrics(resourceName)
}
