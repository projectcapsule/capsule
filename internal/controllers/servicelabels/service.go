// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package servicelabels

import (
	"context"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/internal/controllers/utils"
	"github.com/projectcapsule/capsule/pkg/runtime/predicates"
)

type ServicesLabelsReconciler struct {
	abstractServiceLabelsReconciler

	Log logr.Logger
}

//nolint:dupl
func (r *ServicesLabelsReconciler) SetupWithManager(ctx context.Context, mgr ctrl.Manager, ctrlConfig utils.ControllerOptions) error {
	r.abstractServiceLabelsReconciler = abstractServiceLabelsReconciler{
		obj:    &corev1.Service{},
		client: mgr.GetClient(),
		log:    r.Log,
	}

	return ctrl.NewControllerManagedBy(mgr).
		Named("service").
		For(r.abstractServiceLabelsReconciler.obj, r.abstractServiceLabelsReconciler.forOptionPerInstanceName(ctx)).
		Watches(
			&capsulev1beta2.Tenant{},
			handler.EnqueueRequestsFromMapFunc(r.servicesForTenant),
			builder.WithPredicates(predicates.TenantServiceOptionsChangedPredicate{}),
		).
		Named("capsule/services").
		WithOptions(ctrlConfig.Runtime.ToControllerOptions()).
		Complete(r)
}

func (r *ServicesLabelsReconciler) servicesForTenant(ctx context.Context, obj client.Object) []reconcile.Request {
	tnt, ok := obj.(*capsulev1beta2.Tenant)
	if !ok {
		return nil
	}

	requests := make([]reconcile.Request, 0)

	for _, namespace := range tnt.Status.Namespaces {
		items := &corev1.ServiceList{}
		if err := r.client.List(ctx, items, client.InNamespace(namespace)); err != nil {
			r.Log.Error(err, "failed listing Services for Tenant metadata update", "tenant", tnt.Name, "namespace", namespace)

			continue
		}

		for i := range items.Items {
			requests = append(requests, reconcile.Request{NamespacedName: client.ObjectKeyFromObject(&items.Items[i])})
		}
	}

	return requests
}
