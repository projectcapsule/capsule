// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package tenant

import (
	"context"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/rest"
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
	// Cleanup performs an authoritative namespace read before every destructive
	// write. Give those reads a separate client so a deletion batch cannot drain
	// the namespace-read token bucket used by admission and Tenant provisioning.
	// Keep the existing configured QPS/burst and reuse the manager's transport.
	cleanupConfig := rest.CopyConfig(mgr.GetConfig())
	cleanupConfig.RateLimiter = nil

	cleanupClient, err := client.New(cleanupConfig, client.Options{
		Scheme: mgr.GetScheme(), Mapper: mgr.GetRESTMapper(), HTTPClient: mgr.GetHTTPClient(),
	})
	if err != nil {
		return fmt.Errorf("create namespace cleanup reader: %w", err)
	}

	r.cleanupReader = cleanupClient

	options := config.Runtime.ToControllerOptions()

	return ctrl.NewControllerManagedBy(mgr).
		Named("capsule/tenants/namespace-cleanup").
		For(&corev1.Namespace{}, builder.WithPredicates(predicate.NewPredicateFuncs(func(obj client.Object) bool {
			ns, ok := obj.(*corev1.Namespace)

			return ok && ns.DeletionTimestamp != nil && len(tenant.TenantOwnerReferences(ns)) > 0
		}))).
		WithOptions(options).
		Complete(reconcile.Func(r.reconcileNamespaceCleanup))
}

func (r *Manager) reconcileNamespaceCleanup(ctx context.Context, request reconcile.Request) (reconcile.Result, error) {
	reader := r.cleanupReader
	if reader == nil {
		reader = r.reader
	}

	ns := &corev1.Namespace{}
	if err := reader.Get(ctx, request.NamespacedName, ns); err != nil {
		return reconcile.Result{}, client.IgnoreNotFound(err)
	}

	if ns.DeletionTimestamp == nil {
		return reconcile.Result{}, nil
	}
	// Admission persists the Tenant owner UID on each namespace. Cleanup is
	// scoped to that namespace's UID and owner, even when an in-flight CREATE
	// persisted after Tenant finalization or the Tenant name has been reused.
	// The cleanup helper rechecks that identity before every destructive write.
	// Labels alone never authorize cleanup, and incomplete ownership fails closed.
	refs := tenant.TenantOwnerReferences(ns)
	if ns.UID == "" || len(refs) != 1 || refs[0].Name == "" || refs[0].UID == "" {
		return reconcile.Result{}, nil
	}

	if label := tenant.TenanLabelValue(ns); label != "" && label != refs[0].Name {
		return reconcile.Result{}, nil
	}

	if remaining := namespaceCascadingCleanupGracePeriod - time.Since(ns.DeletionTimestamp.Time); remaining > 0 {
		return reconcile.Result{RequeueAfter: remaining}, nil
	}

	pending, err := tenant.NamespaceIsPendingPodTerminating(ctx, reader, ns)
	if err != nil {
		return reconcile.Result{}, err
	}

	if pending {
		return reconcile.Result{RequeueAfter: namespaceCleanupRetryPeriod}, nil
	}

	err = r.runPhase(ctrl.LoggerFrom(ctx), "namespace_cleanup", func() error {
		_, cleanupErr := tenant.NamespacedCascadingCleanup(ctx, reader, r.DiscoveryClient, &r.discoveryCache, r.DynamicClient, ns)

		return cleanupErr
	})
	if err != nil {
		return reconcile.Result{}, err
	}

	return reconcile.Result{RequeueAfter: namespaceCleanupRetryPeriod}, nil
}
