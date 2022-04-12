package stats

import (
	"github.com/prometheus/client_golang/prometheus"
	"sigs.k8s.io/controller-runtime/pkg/metrics"
)

var activeTenantCount = prometheus.NewCounterVec(
	prometheus.CounterOpts{
		Namespace: "capsule",
		Subsystem: "tenants",
		Name:      "status_active_count",
		Help:      "Total number of active tenants",
	},
	[]string{},
)

var cordonedTenantCount = prometheus.NewCounterVec(
	prometheus.CounterOpts{
		Namespace: "capsule",
		Subsystem: "tenants",
		Name:      "status_cordoned_count",
		Help:      "Total number of cordoned tenants",
	},
	[]string{},
)

// controller-runtime uses metrics.Registry registry for exposed metrics. tenant_count will
// be exposed with the rest of the controller-runtime metrics.
func init() {
	metrics.Registry.MustRegister(activeTenantCount, cordonedTenantCount)
}

// RecordActiveTenant increments capsule_tenants_status_active_count counter
// metric
func RecordActiveTenant() {
	activeTenantCount.With(prometheus.Labels{}).Inc()
}

// RecordCordonedTenant increments capsule_tenants_status_cordoned_count counter
// metric
func RecordCordonedTenant() {
	cordonedTenantCount.With(prometheus.Labels{}).Inc()
}
