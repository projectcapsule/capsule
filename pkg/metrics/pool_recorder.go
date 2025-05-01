// Copyright 2024 Peak Scale
// SPDX-License-Identifier: Apache-2.0

package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	crtlmetrics "sigs.k8s.io/controller-runtime/pkg/metrics"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
)

type ResourcePoolRecorder struct {
	poolResource           *prometheus.GaugeVec
	poolResourceLimit      *prometheus.GaugeVec
	poolResourceUsage      *prometheus.GaugeVec
	namespaceResourceClaim *prometheus.GaugeVec
}

func MustMakeResourcePoolRecorder() *ResourcePoolRecorder {
	metricsRecorder := NewResourcePoolRecorder()
	crtlmetrics.Registry.MustRegister(metricsRecorder.Collectors()...)

	return metricsRecorder
}

func NewResourcePoolRecorder() *ResourcePoolRecorder {
	return &ResourcePoolRecorder{
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
		namespaceResourceClaim: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: metricsPrefix,
				Name:      "pool_namespace_claimed",
				Help:      "Current resources claimed on namespace basis for a given resource in a resource pool for a specific namespace",
			},
			[]string{"pool", "namespace", "resource"},
		),
	}
}

func (r *ResourcePoolRecorder) Collectors() []prometheus.Collector {
	return []prometheus.Collector{
		r.poolResource,
		r.poolResourceLimit,
		r.poolResourceUsage,
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
	}

	r.resourceUsageMetricsByNamespace(pool)
}

// Calculate allocation per namespace for metric
func (r *ResourcePoolRecorder) resourceUsageMetricsByNamespace(pool *capsulev1beta2.ResourcePool) {
	resources := pool.GetClaimedByNamespaceClaims()

	for namespace, claims := range resources {
		for resourceName, quantity := range claims {
			r.namespaceResourceClaim.WithLabelValues(
				pool.Name,
				namespace,
				resourceName.String(),
			).Set(float64(quantity.MilliValue()) / 1000)
		}
	}
}

// Delete all metrics for a namespace in a resource pool.
func (r *ResourcePoolRecorder) DeleteResourcePoolNamespaceMetric(pool string, namespace string) {
	r.namespaceResourceClaim.DeletePartialMatch(map[string]string{"pool": pool, "namespace": namespace})
}

// Delete all metrics for a resource pool.
func (r *ResourcePoolRecorder) DeleteResourcePoolMetric(pool string) {
	r.poolResourceLimit.DeletePartialMatch(map[string]string{"pool": pool})
	r.poolResourceUsage.DeletePartialMatch(map[string]string{"pool": pool})
	r.namespaceResourceClaim.DeletePartialMatch(map[string]string{"pool": pool})
	r.poolResource.DeletePartialMatch(map[string]string{"pool": pool})
}
