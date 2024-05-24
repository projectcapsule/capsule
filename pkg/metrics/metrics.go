// Copyright 2020-2023 Project Capsule Authors.
// SPDX-License-Identifier: Apache-2.0

package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"sigs.k8s.io/controller-runtime/pkg/metrics"
)

var (
	metricsPrefix = "capsule_"

	TenantResourceUsage = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: metricsPrefix + "tenant_resource_usage",
		Help: "Current resource usage for a given resource in a tenant",
	}, []string{"tenant", "resource", "resourcequotaindex"})

	TenantResourceLimit = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: metricsPrefix + "tenant_resource_limit",
		Help: "Current resource limit for a given resource in a tenant",
	}, []string{"tenant", "resource", "resourcequotaindex"})
)

func init() {
	metrics.Registry.MustRegister(
		TenantResourceUsage,
		TenantResourceLimit,
	)
}
