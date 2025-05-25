// Copyright 2020-2023 Project Capsule Authors.
// SPDX-License-Identifier: Apache-2.0

package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	crtlmetrics "sigs.k8s.io/controller-runtime/pkg/metrics"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
)

type ResourcePoolRecorder struct {
	poolResource               *prometheus.GaugeVec
	poolResourceLimit          *prometheus.GaugeVec
	poolResourceAvailable      *prometheus.GaugeVec
	poolResourceUsage          *prometheus.GaugeVec
	poolResourceExhaustion     *prometheus.GaugeVec
	poolNamespaceResourceUsage *prometheus.GaugeVec
}

func MustMakeResourcePoolRecorder() *ResourcePoolRecorder {
	metricsRecorder := NewResourcePoolRecorder()
	crtlmetrics.Registry.MustRegister(metricsRecorder.Collectors()...)

	return metricsRecorder
}

func NewResourcePoolRecorder() *ResourcePoolRecorder {
	return &ResourcePoolRecorder{
		poolResourceExhaustion: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: metricsPrefix,
				Name:      "pool_exhaustion",
				Help:      "Resources become exhausted, when there's not enough available for all claims and the claims get queued",
			},
			[]string{"pool", "resource"},
		),
		poolResource: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: metricsPrefix,
				Name:      "pool_resource",
				Help:      "Type of resource being used in a resource pool",
			},
			[]string{"pool", "resource"},
		),
		poolResourceLimit: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: metricsPrefix,
				Name:      "pool_limit",
				Help:      "Current resource limit for a given resource in a resource pool",
			},
			[]string{"pool", "resource"},
		),
		poolResourceUsage: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: metricsPrefix,
				Name:      "pool_usage",
				Help:      "Current resource usage for a given resource in a resource pool",
			},
			[]string{"pool", "resource"},
		),

		poolResourceAvailable: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: metricsPrefix,
				Name:      "pool_available",
				Help:      "Current resource availability for a given resource in a resource pool",
			},
			[]string{"pool", "resource"},
		),
		poolNamespaceResourceUsage: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: metricsPrefix,
				Name:      "pool_namespace_usage",
				Help:      "Current resources claimed on namespace basis for a given resource in a resource pool for a specific namespace",
			},
			[]string{"pool", "target_namespace", "resource"},
		),
	}
}

func (r *ResourcePoolRecorder) Collectors() []prometheus.Collector {
	return []prometheus.Collector{
		r.poolResource,
		r.poolResourceLimit,
		r.poolResourceUsage,
		r.poolResourceAvailable,
		r.poolResourceExhaustion,
		r.poolNamespaceResourceUsage,
	}
}

// Emit current hard limits and usage for a resource pool.
func (r *ResourcePoolRecorder) ResourceUsageMetrics(pool *capsulev1beta2.ResourcePool) {
	for resourceName, quantity := range pool.Status.Allocation.Hard {
		r.poolResourceLimit.WithLabelValues(
			pool.Name,
			resourceName.String(),
		).Set(float64(quantity.MilliValue()) / 1000)

		r.poolResource.WithLabelValues(
			pool.Name,
			resourceName.String(),
		).Set(float64(1))

		claimed, exists := pool.Status.Allocation.Claimed[resourceName]
		if !exists {
			r.poolResourceUsage.DeletePartialMatch(map[string]string{
				"pool":     pool.Name,
				"resource": resourceName.String(),
			})

			continue
		}

		r.poolResourceUsage.WithLabelValues(
			pool.Name,
			resourceName.String(),
		).Set(float64(claimed.MilliValue()) / 1000)

		available := pool.Status.Allocation.Available[resourceName]
		r.poolResourceAvailable.WithLabelValues(
			pool.Name,
			resourceName.String(),
		).Set(float64(available.MilliValue()) / 1000)
	}

	r.resourceUsageMetricsByNamespace(pool)
}

// Delete all metrics for a namespace in a resource pool.
func (r *ResourcePoolRecorder) DeleteResourcePoolNamespaceMetric(pool string, namespace string) {
	r.poolNamespaceResourceUsage.DeletePartialMatch(map[string]string{"pool": pool, "namespace": namespace})
}

// Delete all metrics for a resource pool.
func (r *ResourcePoolRecorder) DeleteResourcePoolMetric(pool string) {
	r.cleanupAllMetricForLabels(map[string]string{"pool": pool})
}

func (r *ResourcePoolRecorder) DeleteResourcePoolSingleResourceMetric(pool string, resourceName string) {
	r.cleanupAllMetricForLabels(map[string]string{"pool": pool, "resource": resourceName})
}

func (r *ResourcePoolRecorder) cleanupAllMetricForLabels(labels map[string]string) {
	r.poolResourceLimit.DeletePartialMatch(labels)
	r.poolResourceAvailable.DeletePartialMatch(labels)
	r.poolResourceUsage.DeletePartialMatch(labels)
	r.poolNamespaceResourceUsage.DeletePartialMatch(labels)
	r.poolResource.DeletePartialMatch(labels)
	r.poolResourceExhaustion.DeletePartialMatch(labels)
}

// Calculate allocation per namespace for metric.
func (r *ResourcePoolRecorder) resourceUsageMetricsByNamespace(pool *capsulev1beta2.ResourcePool) {
	resources := pool.GetClaimedByNamespaceClaims()

	for namespace, claims := range resources {
		for resourceName, quantity := range claims {
			r.poolNamespaceResourceUsage.WithLabelValues(
				pool.Name,
				namespace,
				resourceName.String(),
			).Set(float64(quantity.MilliValue()) / 1000)
		}
	}
}
