// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package tenant

import (
	"context"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/projectcapsule/capsule/internal/controllers/utils"
	"github.com/projectcapsule/capsule/pkg/tenant"
)

const (
	namespaceCascadingCleanupGracePeriod = 10 * time.Second
	namespaceCleanupRetryPeriod          = 5 * time.Second
)

func (r *Manager) setupNamespaceCleanupController(mgr ctrl.Manager, config utils.ControllerOptions) error {
	options := config.Runtime.ToControllerOptions()
	// Cleanup already fans out to four resource types. A single worker bounds
	// background API pressure independently of Tenant provisioning concurrency.
	options.MaxConcurrentReconciles = 1

	return ctrl.NewControllerManagedBy(mgr).
		Named("capsule/namespace-cleanup").
		For(&corev1.Namespace{}, builder.WithPredicates(predicate.NewPredicateFuncs(func(obj client.Object) bool {
			ns, ok := obj.(*corev1.Namespace)

			return ok && ns.DeletionTimestamp != nil && len(tenant.TenantOwnerReferences(ns)) > 0
		}))).
		WithOptions(options).
		Complete(reconcile.Func(r.reconcileNamespaceCleanup))
}

func (r *Manager) reconcileNamespaceCleanup(ctx context.Context, request reconcile.Request) (reconcile.Result, error) {
	ns := &corev1.Namespace{}
	if err := r.reader.Get(ctx, request.NamespacedName, ns); err != nil {
		return reconcile.Result{}, client.IgnoreNotFound(err)
	}

	if ns.DeletionTimestamp == nil {
		return reconcile.Result{}, nil
	}
	// Labels only select candidates; a live Tenant with the matching owner UID
	// establishes the scope of this controller's destructive operations.
	tnt, err := tenant.ResolveNamespaceTenant(ctx, r.reader, ns)
	if apierrors.IsNotFound(err) {
		return reconcile.Result{}, nil
	}

	if err != nil {
		return reconcile.Result{}, err
	}

	if tnt == nil {
		return reconcile.Result{}, nil
	}

	refs := tenant.TenantOwnerReferences(ns)
	if len(refs) != 1 || tnt.UID == "" || !tenant.IsTenantOwnerReferenceForTenant(refs[0], tnt) {
		return reconcile.Result{}, nil
	}

	if remaining := namespaceCascadingCleanupGracePeriod - time.Since(ns.DeletionTimestamp.Time); remaining > 0 {
		return reconcile.Result{RequeueAfter: remaining}, nil
	}

	pending, err := tenant.NamespaceIsPendingPodTerminating(ctx, r.reader, ns)
	if err != nil {
		return reconcile.Result{}, err
	}

	if pending {
		return reconcile.Result{RequeueAfter: namespaceCleanupRetryPeriod}, nil
	}

	err = r.runPhase(ctrl.LoggerFrom(ctx), "namespace_cleanup", func() error {
		_, cleanupErr := tenant.NamespacedCascadingCleanup(ctx, r.reader, r.DiscoveryClient, &r.discoveryCache, r.DynamicClient, ns)

		return cleanupErr
	})
	if err != nil {
		return reconcile.Result{}, err
	}

	return reconcile.Result{RequeueAfter: namespaceCleanupRetryPeriod}, nil
}
