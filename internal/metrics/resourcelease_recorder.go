// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	crtlmetrics "sigs.k8s.io/controller-runtime/pkg/metrics"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
)

type ResourceLeasesRecorder struct {
	phaseGauge *prometheus.GaugeVec
}

func MustMakeResourceLeasesRecorder() *ResourceLeasesRecorder {
	metricsRecorder := NewResourceLeasesRecorder()
	crtlmetrics.Registry.MustRegister(metricsRecorder.Collectors()...)

	return metricsRecorder
}

func NewResourceLeasesRecorder() *ResourceLeasesRecorder {
	return &ResourceLeasesRecorder{
		phaseGauge: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: metricsPrefix,
				Name:      "resourcelease_phase",
				Help:      "The current phase of the ResourceLease.",
			},
			[]string{"name", "target_namespace", "status"},
		),
	}
}

func (r *ResourceLeasesRecorder) Collectors() []prometheus.Collector {
	return []prometheus.Collector{
		r.phaseGauge,
	}
}

// RecordResourceLeasePhase records the current phase of the ResourceLease.
func (r *ResourceLeasesRecorder) RecordResourceLeasePhase(br *capsulev1beta2.ResourceLease) {
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

// DeleteResourceLeaseMetrics deletes all metrics series for the given ResourceLease.
func (r *ResourceLeasesRecorder) DeleteResourceLeaseMetrics(br *capsulev1beta2.ResourceLease) {
	if r == nil || r.phaseGauge == nil || br == nil {
		return
	}

	r.phaseGauge.DeletePartialMatch(map[string]string{
		"name":             br.GetName(),
		"target_namespace": br.GetNamespace(),
	})
}
