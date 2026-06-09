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
	return &BreakRequestsRecorder{
		requestConditionGauge: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: metricsPrefix,
				Name:      "breakrequest_phase",
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

// RecordRequestCondition records the current phase of the BreakRequest.
func (r *BreakRequestsRecorder) RecordRequestCondition(br *capsulev1beta2.BreakRequest) {
	if r == nil || r.requestConditionGauge == nil || br == nil {
		return
	}
	// Remove previous status series for this request.
	r.requestConditionGauge.DeletePartialMatch(map[string]string{
		"name":             br.GetName(),
		"target_namespace": br.GetNamespace(),
	})

	if br.Status.Phase == "" {
		return
	}

	r.requestConditionGauge.WithLabelValues(br.GetName(), br.GetNamespace(), string(br.Status.Phase)).Set(1)
}

// DeleteRequestMetrics deletes all metrics series for the given BreakRequest.
func (r *BreakRequestsRecorder) DeleteRequestMetrics(br *capsulev1beta2.BreakRequest) {
	if r == nil || r.requestConditionGauge == nil || br == nil {
		return
	}

	r.requestConditionGauge.DeletePartialMatch(map[string]string{
		"name":             br.GetName(),
		"target_namespace": br.GetNamespace(),
	})
}
