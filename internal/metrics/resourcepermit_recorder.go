// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	crtlmetrics "sigs.k8s.io/controller-runtime/pkg/metrics"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
)

type ResourcePermitsRecorder struct {
	phaseGauge *prometheus.GaugeVec
}

func MustMakeResourcePermitsRecorder() *ResourcePermitsRecorder {
	metricsRecorder := NewResourcePermitsRecorder()
	crtlmetrics.Registry.MustRegister(metricsRecorder.Collectors()...)

	return metricsRecorder
}

func NewResourcePermitsRecorder() *ResourcePermitsRecorder {
	return &ResourcePermitsRecorder{
		phaseGauge: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: metricsPrefix,
				Name:      "resourcepermit_phase",
				Help:      "The current phase of the ResourcePermit.",
			},
			[]string{"name", "target_namespace", "status"},
		),
	}
}

func (r *ResourcePermitsRecorder) Collectors() []prometheus.Collector {
	return []prometheus.Collector{
		r.phaseGauge,
	}
}

// RecordResourcePermitPhase records the current phase of the ResourcePermit.
func (r *ResourcePermitsRecorder) RecordResourcePermitPhase(br *capsulev1beta2.ResourcePermit) {
	if r == nil || r.phaseGauge == nil || br == nil {
		return
	}
	// Remove previous status series for this request.
	r.phaseGauge.DeletePartialMatch(map[string]string{
		"name":             br.GetName(),
		"target_namespace": br.GetNamespace(),
	})

	if br.Status.Phase == "" {
		return
	}

	r.phaseGauge.WithLabelValues(br.GetName(), br.GetNamespace(), string(br.Status.Phase)).Set(1)
}

// DeleteResourcePermitMetrics deletes all metrics series for the given ResourcePermit.
func (r *ResourcePermitsRecorder) DeleteResourcePermitMetrics(br *capsulev1beta2.ResourcePermit) {
	if r == nil || r.phaseGauge == nil || br == nil {
		return
	}

	r.phaseGauge.DeletePartialMatch(map[string]string{
		"name":             br.GetName(),
		"target_namespace": br.GetNamespace(),
	})
}
