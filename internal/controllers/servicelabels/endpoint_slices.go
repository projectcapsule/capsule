// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package servicelabels

import (
	"context"

	"github.com/go-logr/logr"
	discoveryv1 "k8s.io/api/discovery/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/internal/controllers/utils"
	"github.com/projectcapsule/capsule/pkg/runtime/predicates"
)

type EndpointSlicesLabelsReconciler struct {
	abstractServiceLabelsReconciler

	Log          logr.Logger
	VersionMinor uint
	VersionMajor uint
}

func (r *EndpointSlicesLabelsReconciler) SetupWithManager(ctx context.Context, mgr ctrl.Manager, ctrlConfig utils.ControllerOptions) error {
	r.abstractServiceLabelsReconciler = abstractServiceLabelsReconciler{
		obj:    &discoveryv1.EndpointSlice{},
		client: mgr.GetClient(),
		log:    r.Log,
	}

	return ctrl.NewControllerManagedBy(mgr).
		Named("endpointslices").
		For(r.abstractServiceLabelsReconciler.obj, r.abstractServiceLabelsReconciler.forOptionPerInstanceName(ctx)).
		Watches(
			&capsulev1beta2.Tenant{},
			handler.EnqueueRequestsFromMapFunc(r.endpointSlicesForTenant),
			builder.WithPredicates(predicates.TenantServiceOptionsChangedPredicate{}),
		).
		Named("capsule/endpointslices").
		WithOptions(ctrlConfig.Runtime.ToControllerOptions()).
		Complete(r)
}

func (r *EndpointSlicesLabelsReconciler) endpointSlicesForTenant(ctx context.Context, obj client.Object) []reconcile.Request {
	tnt, ok := obj.(*capsulev1beta2.Tenant)
	if !ok {
		return nil
	}

	requests := make([]reconcile.Request, 0)
	for _, namespace := range tnt.Status.Namespaces {
		items := &discoveryv1.EndpointSliceList{}
		if err := r.client.List(ctx, items, client.InNamespace(namespace)); err != nil {
			r.Log.Error(err, "failed listing EndpointSlices for Tenant metadata update", "tenant", tnt.Name, "namespace", namespace)

			continue
		}

		for i := range items.Items {
			requests = append(requests, reconcile.Request{NamespacedName: client.ObjectKeyFromObject(&items.Items[i])})
		}
	}

	return requests
}
