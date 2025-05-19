// Copyright 2020-2023 Project Capsule Authors.
// SPDX-License-Identifier: Apache-2.0

package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	crtlmetrics "sigs.k8s.io/controller-runtime/pkg/metrics"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/meta"
)

type ClaimRecorder struct {
	claimConditionGauge *prometheus.GaugeVec
}

func MustMakeClaimRecorder() *ClaimRecorder {
	metricsRecorder := NewClaimRecorder()
	crtlmetrics.Registry.MustRegister(metricsRecorder.Collectors()...)

	return metricsRecorder
}

func NewClaimRecorder() *ClaimRecorder {
	return &ClaimRecorder{
		claimConditionGauge: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: metricsPrefix,
				Name:      "claim_condition",
				Help:      "The current condition status of a claim.",
			},
			[]string{"name", "reason", "status", "pool"},
		),
	}
}

func (r *ClaimRecorder) Collectors() []prometheus.Collector {
	return []prometheus.Collector{
		r.claimConditionGauge,
	}
}

// RecordCondition records the condition as given for the ref.
func (r *ClaimRecorder) RecordClaimCondition(claim *capsulev1beta2.ResourcePoolClaim) {
	for _, status := range []string{meta.ReadyCondition, meta.NotReadyCondition} {
		var value float64
		if status == string(claim.Status.Condition.Status) {
			value = 1
		}

		r.claimConditionGauge.WithLabelValues(
			claim.Name,
			claim.Status.Condition.Reason,
			status,
			claim.Status.Pool.Name.String(),
		).Set(value)
	}
}

// DeleteCondition deletes the condition metrics for the ref.
func (r *ClaimRecorder) DeleteClaimMetric(claim string) {
	for _, status := range []string{meta.ReadyCondition, meta.NotReadyCondition} {
		r.claimConditionGauge.DeleteLabelValues(claim, status)
	}
}
