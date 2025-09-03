// Copyright 2020-2023 Project Capsule Authors.
// SPDX-License-Identifier: Apache-2.0

package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	crtlmetrics "sigs.k8s.io/controller-runtime/pkg/metrics"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
)

type TenantResourceRecorder struct {
	resourceConditionGauge *prometheus.GaugeVec
}

func MustMakeTenantResourceRecorder() *TenantResourceRecorder {
	metricsRecorder := NewTenantResourceRecorder()
	crtlmetrics.Registry.MustRegister(metricsRecorder.Collectors()...)

	return metricsRecorder
}

func NewTenantResourceRecorder() *TenantResourceRecorder {
	return &TenantResourceRecorder{
		resourceConditionGauge: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: metricsPrefix,
				Name:      "resource_condition",
				Help:      "The current condition status of a tenant resource.",
			},
			[]string{"name", "target_namespace", "condition", "status"},
		),
	}
}

func (r *TenantResourceRecorder) Collectors() []prometheus.Collector {
	return []prometheus.Collector{
		r.resourceConditionGauge,
	}
}

// RecordCondition records the condition as given for the ref.
func (r *TenantResourceRecorder) RecordCondition(resource *capsulev1beta2.TenantResource) {
	for _, status := range []metav1.ConditionStatus{metav1.ConditionTrue, metav1.ConditionFalse, metav1.ConditionUnknown} {
		var value float64
		if status == resource.Status.Condition.Status {
			value = 1
		}

		r.resourceConditionGauge.WithLabelValues(
			resource.Name,
			resource.Namespace,
			resource.Status.Condition.Type,
			string(resource.Status.Condition.Status),
		).Set(value)
	}
}

// DeleteCondition deletes the condition metrics for the ref.
func (r *TenantResourceRecorder) DeleteMetrics(resourceName string, resourceNamespace string) {
	r.resourceConditionGauge.DeletePartialMatch(map[string]string{
		"name":             resourceName,
		"target_namespace": resourceNamespace,
	})
}
