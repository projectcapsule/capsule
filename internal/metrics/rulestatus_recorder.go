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

type RuleStatusRecorder struct {
	resourceConditionGauge *prometheus.GaugeVec
}

func MustMakeRuleStatusRecorder() *RuleStatusRecorder {
	metricsRecorder := NewRuleStatusRecorder()
	crtlmetrics.Registry.MustRegister(metricsRecorder.Collectors()...)

	return metricsRecorder
}

func NewRuleStatusRecorder() *RuleStatusRecorder {
	return &RuleStatusRecorder{
		resourceConditionGauge: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: metricsPrefix,
				Name:      "rulestatus_condition",
				Help:      "The current condition status of a rulestatus resource.",
			},
			[]string{"name", "target_namespace", "condition"},
		),
	}
}

func (r *RuleStatusRecorder) Collectors() []prometheus.Collector {
	return []prometheus.Collector{
		r.resourceConditionGauge,
	}
}

// RecordCondition records the condition as given for the ref.
func (r *RuleStatusRecorder) RecordConditions(resource *capsulev1beta2.RuleStatus) {
	for _, status := range []string{meta.ReadyCondition, meta.CordonedCondition} {
		var value float64

		cond := resource.Status.Conditions.GetConditionByType(status)
		if cond == nil {
			r.DeleteConditionMetricByType(resource.GetName(), resource.GetNamespace(), status)

			continue
		}

		if cond.Status == metav1.ConditionTrue {
			value = 1
		}

		r.resourceConditionGauge.WithLabelValues(resource.GetName(), resource.GetNamespace(), status).Set(value)
	}
}

func (r *RuleStatusRecorder) DeleteConditionMetrics(name string, namespace string) {
	r.resourceConditionGauge.DeletePartialMatch(map[string]string{
		"name":             name,
		"target_namespace": namespace,
	})
}

func (r *RuleStatusRecorder) DeleteConditionMetricByType(name string, namespace string, condition string) {
	r.resourceConditionGauge.DeletePartialMatch(map[string]string{
		"name":             name,
		"target_namespace": namespace,
		"condition":        condition,
	})
}

// DeleteCondition deletes the condition metrics for the ref.
func (r *RuleStatusRecorder) DeleteMetrics(resourceName string, resourceNamespace string) {
	r.resourceConditionGauge.DeletePartialMatch(map[string]string{
		"name":             resourceName,
		"target_namespace": resourceNamespace,
	})

	r.DeleteConditionMetrics(resourceName, resourceNamespace)
}
