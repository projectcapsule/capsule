// Copyright 2020-2025 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	crtlmetrics "sigs.k8s.io/controller-runtime/pkg/metrics"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api"
)

type ResourcePoolRecorder struct {
	poolResource                         *prometheus.GaugeVec
	poolResourceLimit                    *prometheus.GaugeVec
	poolResourceAvailable                *prometheus.GaugeVec
	poolResourceUsage                    *prometheus.GaugeVec
	poolResourceUsagePercentage          *prometheus.GaugeVec
	poolResourceExhaustion               *prometheus.GaugeVec
	poolResourceExhaustionPercentage     *prometheus.GaugeVec
	poolNamespaceResourceUsage           *prometheus.GaugeVec
	poolNamespaceResourceUsagePercentage *prometheus.GaugeVec
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
		poolResourceExhaustionPercentage: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: metricsPrefix,
				Name:      "pool_exhaustion_percentage",
				Help:      "Resources become exhausted, when there's not enough available for all claims and the claims get queued (Percentage)",
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
		poolResourceUsagePercentage: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: metricsPrefix,
				Name:      "pool_usage_percentage",
				Help:      "Current resource usage for a given resource in a resource pool (percentage)",
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
		poolNamespaceResourceUsagePercentage: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: metricsPrefix,
				Name:      "pool_namespace_usage_percentage",
				Help:      "Current resources claimed on namespace basis for a given resource in a resource pool for a specific namespace (percentage)",
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
		r.poolResourceUsagePercentage,
		r.poolResourceAvailable,
		r.poolResourceExhaustion,
		r.poolResourceExhaustionPercentage,
		r.poolNamespaceResourceUsage,
		r.poolNamespaceResourceUsagePercentage,
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

		usagePercentage := float64(0)
		if quantity.MilliValue() > 0 {
			usagePercentage = (float64(claimed.MilliValue()) / float64(quantity.MilliValue())) * 100
		}

		r.poolResourceUsagePercentage.WithLabelValues(
			pool.Name,
			resourceName.String(),
		).Set(usagePercentage)
	}

	r.resourceUsageMetricsByNamespace(pool)
}

// Emit exhaustion metrics.
func (r *ResourcePoolRecorder) CalculateExhaustions(
	pool *capsulev1beta2.ResourcePool,
	current map[string]api.PoolExhaustionResource,
) {
	for resource := range pool.Status.Exhaustions {
		if _, ok := current[resource]; ok {
			continue
		}

		r.poolResourceExhaustion.DeleteLabelValues(pool.Name, resource)
		r.poolResourceExhaustionPercentage.DeleteLabelValues(pool.Name, resource)
	}

	for resource, ex := range current {
		available := float64(ex.Available.MilliValue()) / 1000
		requesting := float64(ex.Requesting.MilliValue()) / 1000

		r.poolResourceExhaustion.WithLabelValues(
			pool.Name,
			resource,
		).Set(float64(ex.Requesting.MilliValue()) / 1000)

		// Calculate and expose overprovisioning percentage
		if available > 0 && requesting > available {
			percent := ((requesting - available) / available) * 100
			r.poolResourceExhaustionPercentage.WithLabelValues(
				pool.Name,
				resource,
			).Set(percent)
		} else {
			r.poolResourceExhaustionPercentage.DeleteLabelValues(pool.Name, resource)
		}
	}
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
	r.poolResourceUsagePercentage.DeletePartialMatch(labels)
	r.poolNamespaceResourceUsage.DeletePartialMatch(labels)
	r.poolNamespaceResourceUsagePercentage.DeletePartialMatch(labels)
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

			available, ok := pool.Status.Allocation.Hard[resourceName]
			if !ok {
				continue
			}

			r.poolNamespaceResourceUsagePercentage.WithLabelValues(
				pool.Name,
				namespace,
				resourceName.String(),
			).Set((float64(quantity.MilliValue()) / float64(available.MilliValue())) * 100)
		}
	}
}
