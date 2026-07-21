// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package tenant

import (
	"context"

	"k8s.io/client-go/util/workqueue"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
)

type tenantClassCollector func(context.Context, *capsulev1beta2.Tenant) error

func (r *Manager) tenantClassEventHandler(collector tenantClassCollector) handler.Funcs {
	syncClasses := func(ctx context.Context) {
		r.syncTenantClasses(ctx, collector)
	}

	return handler.Funcs{
		CreateFunc: func(
			ctx context.Context,
			_ event.TypedCreateEvent[client.Object],
			_ workqueue.TypedRateLimitingInterface[reconcile.Request],
		) {
			syncClasses(ctx)
		},
		UpdateFunc: func(
			ctx context.Context,
			_ event.TypedUpdateEvent[client.Object],
			_ workqueue.TypedRateLimitingInterface[reconcile.Request],
		) {
			syncClasses(ctx)
		},
		DeleteFunc: func(
			ctx context.Context,
			_ event.TypedDeleteEvent[client.Object],
			_ workqueue.TypedRateLimitingInterface[reconcile.Request],
		) {
			syncClasses(ctx)
		},
	}
}

func (r *Manager) syncTenantClasses(ctx context.Context, collector tenantClassCollector) {
	var tenants capsulev1beta2.TenantList
	if err := r.List(ctx, &tenants); err != nil {
		r.Log.Error(err, "cannot list Tenants for class sync")

		return
	}

	for i := range tenants.Items {
		name := tenants.Items[i].Name
		if err := r.updateTenantClassStatus(ctx, name, collector); err != nil {
			r.Log.Error(err, "cannot update Tenant class status", "tenant", name)
		}
	}
}
