// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package tenant

import (
	"context"
	"slices"

	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"
	"k8s.io/client-go/util/workqueue"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/runtime/predicates"
	"github.com/projectcapsule/capsule/pkg/utils"
)

func (r *Manager) statusOnlyHandlerClasses(
	fn func(ctx context.Context, perTenant func(context.Context, *capsulev1beta2.Tenant) error) error,
	perTenant func(context.Context, *capsulev1beta2.Tenant) error,
	errMsg string,
) *handler.TypedFuncs[client.Object, reconcile.Request] {
	return &handler.TypedFuncs[client.Object, reconcile.Request]{
		CreateFunc: func(
			ctx context.Context,
			_ event.TypedCreateEvent[client.Object],
			_ workqueue.TypedRateLimitingInterface[reconcile.Request],
		) {
			if err := fn(ctx, perTenant); err != nil {
				r.Log.Error(err, errMsg)
			}
		},
		UpdateFunc: func(
			ctx context.Context,
			_ event.TypedUpdateEvent[client.Object],
			_ workqueue.TypedRateLimitingInterface[reconcile.Request],
		) {
			if err := fn(ctx, perTenant); err != nil {
				r.Log.Error(err, errMsg)
			}
		},
		DeleteFunc: func(
			ctx context.Context,
			_ event.TypedDeleteEvent[client.Object],
			_ workqueue.TypedRateLimitingInterface[reconcile.Request],
		) {
			if err := fn(ctx, perTenant); err != nil {
				r.Log.Error(err, errMsg)
			}
		},
	}
}

func (r *Manager) statusOnlyHandlerNodes() *handler.TypedFuncs[client.Object, reconcile.Request] {
	metadataPredicate := predicates.UpdatedMetadataPredicate{}

	return &handler.TypedFuncs[client.Object, reconcile.Request]{
		CreateFunc: func(
			ctx context.Context,
			e event.TypedCreateEvent[client.Object],
			_ workqueue.TypedRateLimitingInterface[reconcile.Request],
		) {
			r.reconcileNodeStatusForMatchingTenants(ctx, e.Object, func(tnt *capsulev1beta2.Tenant, node client.Object) bool {
				return tenantNodeSelectorMatch(tnt, node)
			})
		},
		UpdateFunc: func(
			ctx context.Context,
			e event.TypedUpdateEvent[client.Object],
			_ workqueue.TypedRateLimitingInterface[reconcile.Request],
		) {
			if !metadataPredicate.Update(event.UpdateEvent{
				ObjectOld: e.ObjectOld,
				ObjectNew: e.ObjectNew,
			}) {
				return
			}

			r.reconcileNodeStatusForMatchingTenants(ctx, e.ObjectNew, func(tnt *capsulev1beta2.Tenant, node client.Object) bool {
				return tenantNodeSelectorMatch(tnt, node) || slices.Contains(tnt.Status.Nodes, node.GetName())
			})
		},
		DeleteFunc: func(
			ctx context.Context,
			e event.TypedDeleteEvent[client.Object],
			_ workqueue.TypedRateLimitingInterface[reconcile.Request],
		) {
			r.reconcileNodeStatusForMatchingTenants(ctx, e.Object, func(tnt *capsulev1beta2.Tenant, node client.Object) bool {
				return slices.Contains(tnt.Status.Nodes, node.GetName())
			})
		},
	}
}

func (r *Manager) reconcileNodeStatusForMatchingTenants(
	ctx context.Context,
	node client.Object,
	tenantMatchFn func(*capsulev1beta2.Tenant, client.Object) bool,
) {
	if node == nil {
		return
	}

	var tenants capsulev1beta2.TenantList
	if err := r.List(ctx, &tenants); err != nil {
		r.Log.Error(err, "failed to list Tenants for node event")

		return
	}

	for i := range tenants.Items {
		tnt := &tenants.Items[i]
		if !tenantMatchFn(tnt, node) {
			continue
		}

		if err := r.collectAvailableNodes(ctx, tnt); err != nil {
			r.Log.Error(err, "cannot collect node status", "tenant", tnt.GetName(), "node", node.GetName())

			continue
		}

		if err := r.updateTenantStatus(ctx, tnt, nil); err != nil {
			r.Log.Error(err, "cannot update tenant status", "tenant", tnt.GetName())
		}
	}
}

func (r *Manager) enqueueTenantsForTenantOwner(
	ctx context.Context,
	tenantOwner client.Object,
	q workqueue.TypedRateLimitingInterface[reconcile.Request],
) {
	var tenants capsulev1beta2.TenantList
	if err := r.List(ctx, &tenants); err != nil {
		r.Log.Error(err, "failed to list Tenants for Tenant Owner event")

		return
	}

	owner, ok := tenantOwner.(*capsulev1beta2.TenantOwner)
	if !ok {
		return
	}

	for i := range tenants.Items {
		tnt := &tenants.Items[i]

		if _, found := tnt.Status.Owners.FindOwner(
			owner.Spec.Name,
			owner.Spec.Kind,
		); !found {
			continue
		}

		q.Add(reconcile.Request{
			NamespacedName: types.NamespacedName{
				Name: tnt.Name,
			},
		})
	}
}

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
		r.Log.Error(err, "failed to list Tenants for class event")

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

// pruningResources is taking care of removing the no more requested sub-resources as LimitRange, ResourceQuota or
// NetworkPolicy using the "exists" and "notin" LabelSelector to perform an outer-join removal.
func (r *Manager) pruningResources(ctx context.Context, ns string, keys []string, obj client.Object) (err error) {
	var capsuleLabel string

	if capsuleLabel, err = utils.GetTypeLabel(obj); err != nil {
		return err
	}

	selector := labels.NewSelector()

	var exists *labels.Requirement

	if exists, err = labels.NewRequirement(capsuleLabel, selection.Exists, []string{}); err != nil {
		return err
	}

	selector = selector.Add(*exists)

	if len(keys) > 0 {
		var notIn *labels.Requirement

		if notIn, err = labels.NewRequirement(capsuleLabel, selection.NotIn, keys); err != nil {
			return err
		}

		selector = selector.Add(*notIn)
	}

	r.Log.V(4).Info("pruning objects with label selector " + selector.String())

	return retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		return r.DeleteAllOf(ctx, obj, &client.DeleteAllOfOptions{
			ListOptions: client.ListOptions{
				LabelSelector: selector,
				Namespace:     ns,
			},
			DeleteOptions: client.DeleteOptions{},
		})
	})
}
