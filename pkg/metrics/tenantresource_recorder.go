// Copyright 2020-2023 Project Capsule Authors.
// SPDX-License-Identifier: Apache-2.0

package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	crtlmetrics "sigs.k8s.io/controller-runtime/pkg/metrics"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/meta"
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
			[]string{"name", "target_namespace", "condition", "status", "reason"},
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
	for _, status := range []string{meta.ReadyCondition} {
		var value float64
		if status == resource.Status.Condition.Type {
			value = 1
		}

		r.resourceConditionGauge.WithLabelValues(
			resource.Name,
			resource.Namespace,
			status,
			string(resource.Status.Condition.Status),
			resource.Status.Condition.Reason,
		).Set(value)
	}
}

// DeleteCondition deletes the condition metrics for the ref.
func (r *TenantResourceRecorder) DeleteMetric(resourceName string) {
	for _, status := range []string{meta.ReadyCondition} {
		r.resourceConditionGauge.DeleteLabelValues(resourceName, status)
	}
}
