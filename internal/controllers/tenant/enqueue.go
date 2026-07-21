// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package tenant

import (
	"context"

	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/workqueue"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
)

func (r *Manager) enqueueForTenantsWithCondition(
	ctx context.Context,
	obj client.Object,
	q workqueue.TypedRateLimitingInterface[reconcile.Request],
	fn func(*capsulev1beta2.Tenant, client.Object) bool,
) {
	var tenants capsulev1beta2.TenantList
	if err := r.List(ctx, &tenants); err != nil {
		r.Log.Error(err, "failed to list Tenants for class event")

		return
	}

	for i := range tenants.Items {
		tnt := &tenants.Items[i]

		if !fn(tnt, obj) {
			continue
		}

		q.Add(reconcile.Request{
			NamespacedName: types.NamespacedName{
				Name: tnt.Name,
			},
		})
	}
}

func (r *Manager) enqueueAllTenants(ctx context.Context, _ client.Object) []reconcile.Request {
	var tenants capsulev1beta2.TenantList
	if err := r.List(ctx, &tenants); err != nil {
		r.Log.Error(err, "failed to list Tenants")

		return nil
	}

	reqs := make([]reconcile.Request, 0, len(tenants.Items))
	for i := range tenants.Items {
		reqs = append(reqs, reconcile.Request{
			NamespacedName: types.NamespacedName{
				Name: tenants.Items[i].Name,
			},
		})
	}

	return reqs
}
