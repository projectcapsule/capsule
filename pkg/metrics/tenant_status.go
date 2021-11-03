package metrics

import (
	"context"

	"github.com/prometheus/client_golang/prometheus"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/clastix/capsule/api/v1beta1"
	"github.com/clastix/capsule/pkg/indexer/tenant"
)

func NewActiveTenantCollector(ctx context.Context, clt client.Client) prometheus.Collector {
	return prometheus.NewCounterFunc(prometheus.CounterOpts{
		Namespace: "capsule",
		Subsystem: "tenant",
		Name:      "active",
		Help:      "sum of active Tenant resources in Active state",
	}, func() float64 {
		list, err := tenant.ListByStatus(ctx, clt, string(v1beta1.TenantStateActive))
		if err != nil {
			return -1
		}

		return float64(len(list.Items))
	})
}

func NewCordonedTenantCollector(ctx context.Context, clt client.Client) prometheus.Collector {
	return prometheus.NewCounterFunc(prometheus.CounterOpts{
		Namespace: "capsule",
		Subsystem: "tenant",
		Name:      "cordoned",
		Help:      "sum of Tenant resources in Cordoned state",
	}, func() float64 {
		list, err := tenant.ListByStatus(ctx, clt, string(v1beta1.TenantStateCordoned))
		if err != nil {
			return -1
		}

		return float64(len(list.Items))
	})
}
