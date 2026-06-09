// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	crtlmetrics "sigs.k8s.io/controller-runtime/pkg/metrics"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
)

type BreakRequestsRecorder struct {
	requestConditionGauge *prometheus.GaugeVec
}

func MustMakeBreakRequestsRecorder() *BreakRequestsRecorder {
	metricsRecorder := NewBreakRequestsRecorder()
	crtlmetrics.Registry.MustRegister(metricsRecorder.Collectors()...)

	return metricsRecorder
}

func NewBreakRequestsRecorder() *BreakRequestsRecorder {
	namespace := "break_requests"

	return &BreakRequestsRecorder{
		requestConditionGauge: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Name:      "phase",
				Help:      "The current phase of the BreakRequest.",
			},
			[]string{"name", "target_namespace", "status"},
		),
	}
}

func (r *BreakRequestsRecorder) Collectors() []prometheus.Collector {
	return []prometheus.Collector{
		r.requestConditionGauge,
	}
}

// RecordRequestCondition RecordCondition records the condition as given for the ref.
func (r *BreakRequestsRecorder) RecordRequestCondition(_ *capsulev1beta2.BreakRequest) {}

// DeleteRequestMetrics DeleteCondition deletes the condition metrics for the ref.
func (r *BreakRequestsRecorder) DeleteRequestMetrics(_ *capsulev1beta2.BreakRequest) {}
