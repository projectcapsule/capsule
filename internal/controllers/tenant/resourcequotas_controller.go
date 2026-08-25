// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package tenant

import (
	"context"
	"errors"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	k8smeta "k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/internal/controllers/utils"
	"github.com/projectcapsule/capsule/pkg/runtime/predicates"
)

func (r *Manager) setupResourceQuotaController(mgr ctrl.Manager, ctrlConfig utils.ControllerOptions) error {
	// Keep ResourceQuota work behind a controller queue. Informer handlers must
	// process their initial object list before controller workers can start, so
	// running a Tenant-wide quota sync directly from a handler can prevent the
	// source from ever reporting that it has synced.
	return ctrl.NewControllerManagedBy(mgr).
		Named("capsule/tenant-resourcequotas").
		Watches(
			&corev1.ResourceQuota{},
			resourceQuotaEventHandler(mgr.GetScheme(), mgr.GetRESTMapper()),
			builder.WithPredicates(predicate.Or(
				predicates.TenantManagedResourceChangedPredicate{},
				predicates.ResourceQuotaUsageChangedPredicate{},
			)),
		).
		WithOptions(ctrlConfig.Runtime.ToControllerOptions()).
		Complete(reconcile.Func(r.reconcileResourceQuotas))
}

func resourceQuotaEventHandler(scheme *runtime.Scheme, mapper k8smeta.RESTMapper) handler.EventHandler {
	return handler.EnqueueRequestForOwner(
		scheme,
		mapper,
		&capsulev1beta2.Tenant{},
		handler.OnlyControllerOwner(),
	)
}

func (r *Manager) reconcileResourceQuotas(
	ctx context.Context,
	request reconcile.Request,
) (reconcile.Result, error) {
	reader := r.reader
	if reader == nil {
		reader = r.Client
	}

	tenant := &capsulev1beta2.Tenant{}
	if err := reader.Get(ctx, client.ObjectKey{Name: request.Name}, tenant); err != nil {
		if apierrors.IsNotFound(err) {
			return reconcile.Result{}, nil
		}

		return reconcile.Result{}, fmt.Errorf("retrieve Tenant for ResourceQuota sync: %w", err)
	}

	if tenant.DeletionTimestamp != nil {
		return reconcile.Result{}, nil
	}

	var errs []error

	if err := r.syncCustomResourceQuotaUsages(ctx, tenant); err != nil {
		errs = append(errs, fmt.Errorf("update custom ResourceQuota usages: %w", err))
	}

	if err := r.syncResourceQuotas(ctx, r.Log, tenant); err != nil {
		errs = append(errs, fmt.Errorf("sync ResourceQuotas: %w", err))
	}

	return reconcile.Result{}, errors.Join(errs...)
}
