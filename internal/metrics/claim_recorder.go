// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	crtlmetrics "sigs.k8s.io/controller-runtime/pkg/metrics"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/meta"
)

type ClaimRecorder struct {
	claimConditions *prometheus.GaugeVec
	claimResources  *prometheus.GaugeVec
	claimPool       *prometheus.GaugeVec
}

func MustMakeClaimRecorder() *ClaimRecorder {
	metricsRecorder := NewClaimRecorder()
	crtlmetrics.Registry.MustRegister(metricsRecorder.Collectors()...)

	return metricsRecorder
}

func NewClaimRecorder() *ClaimRecorder {
	return &ClaimRecorder{
		claimPool: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: metricsPrefix,
				Name:      "claim_pool",
				Help:      "The current assigned pool of a claim.",
			},
			[]string{"name", "target_namespace", "pool"},
		),

		claimConditions: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: metricsPrefix,
				Name:      "claim_condition",
				Help:      "The current condition status of a claim.",
			},
			[]string{"name", "target_namespace", "condition"},
		),
		claimResources: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: metricsPrefix,
				Name:      "claim_resource",
				Help:      "The given amount of resources from the claim",
			},
			[]string{"name", "target_namespace", "resource"},
		),
	}
}

func (r *ClaimRecorder) Collectors() []prometheus.Collector {
	return []prometheus.Collector{
		r.claimConditions,
		r.claimResources,
		r.claimPool,
	}
}

// RecordCondition records the condition as given for the ref.
func (r *ClaimRecorder) RecordClaimCondition(claim *capsulev1beta2.ResourcePoolClaim) {
	// Record Condition Metrics
	for _, status := range []string{meta.ReadyCondition, meta.ExhaustedCondition, meta.BoundCondition} {
		var value float64

		cond := claim.Status.Conditions.GetConditionByType(status)
		if cond == nil {
			r.DeleteConditionMetricByType(claim.Name, claim.Namespace, status)

			continue
		}

		if cond.Status == metav1.ConditionTrue {
			value = 1
		}

		r.claimConditions.WithLabelValues(claim.Name, claim.Namespace, status).Set(value)
	}

	// Record Pool Association
	r.claimPool.WithLabelValues(claim.Name, claim.Namespace, claim.GetPool()).Set(1)

	// Record requested resources
	r.claimResources.DeletePartialMatch(map[string]string{
		"name":             claim.Name,
		"target_namespace": claim.Namespace,
	})

	for resourceName, qt := range claim.Spec.ResourceClaims {
		r.claimResources.WithLabelValues(
			claim.Name,
			claim.Namespace,
			resourceName.String(),
		).Set(float64(qt.MilliValue()) / 1000)
	}
}

func (r *ClaimRecorder) DeleteConditionMetricByType(claim string, namespace string, condition string) {
	r.claimConditions.Delete(map[string]string{
		"name":             claim,
		"target_namespace": namespace,
		"condition":        condition,
	})
}

func (r *ClaimRecorder) DeletePoolAssociation(claim string, namespace string, pool string) {
	r.claimPool.Delete(map[string]string{
		"name":             claim,
		"target_namespace": namespace,
		"pool":             pool,
	})
}

// DeleteCondition deletes the condition metrics for the ref.
func (r *ClaimRecorder) DeleteClaimMetric(claim string, namespace string) {
	r.claimConditions.DeletePartialMatch(map[string]string{
		"name":             claim,
		"target_namespace": namespace,
	})
	r.claimResources.DeletePartialMatch(map[string]string{
		"name":             claim,
		"target_namespace": namespace,
	})
	r.claimPool.DeletePartialMatch(map[string]string{
		"name":             claim,
		"target_namespace": namespace,
	})
}
