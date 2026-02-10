// Copyright 2020-2025 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package serviceaccounts

import (
	"context"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/internal/cache"
	"github.com/projectcapsule/capsule/internal/controllers/utils"
	"github.com/projectcapsule/capsule/pkg/runtime/configuration"
	"github.com/projectcapsule/capsule/pkg/runtime/indexers/tenantresource"
)

type Manager struct {
	client.Client
	Log           logr.Logger
	Configuration configuration.Configuration
	Cache         *cache.ImpersonationCache
}

func (r *Manager) SetupWithManager(mgr ctrl.Manager, ctrlConfig utils.ControllerOptions) error {
	return ctrl.NewControllerManagedBy(mgr).
		Named("cache/serviceaccounts").
		For(&corev1.ServiceAccount{}).
		Complete(r)
}

func (r *Manager) Reconcile(ctx context.Context, request reconcile.Request) (res reconcile.Result, err error) {
	instance := &corev1.ServiceAccount{}
	if err = r.Client.Get(ctx, request.NamespacedName, instance); err != nil && !apierrors.IsNotFound(err) {
		r.Log.Error(err, "Error reading the object")

		return res, err
	}

	defer func() {
		r.Log.V(5).Info("checking chache references for serviceaccount", "name", request.Name, "namespace", request.Namespace)

		r.Log.V(5).Info("client cache stats", "size", r.Cache.Stats())
	}()

	if apierrors.IsNotFound(err) || !instance.ObjectMeta.DeletionTimestamp.IsZero() {
		r.Log.V(4).Info("invalidating cache for serviceaccount cache", "name", request.Name, "namespace", request.Namespace)

		r.Cache.Invalidate(request.Namespace, request.Name)

		return res, nil
	}

	hasReference, err := r.checkReferences(ctx, instance)
	if err != nil {
		return res, err
	}

	if !hasReference {
		r.Log.V(4).Info("invalidating cache for serviceaccount cache", "name", request.Name, "namespace", request.Namespace)

		r.Cache.Invalidate(request.Namespace, request.Name)
	}

	return reconcile.Result{
		Requeue:      true,
		RequeueAfter: r.Configuration.CacheInvalidation().Duration,
	}, nil
}

func (r *Manager) checkReferences(
	ctx context.Context,
	sa *corev1.ServiceAccount,
) (ref bool, err error) {
	key := sa.GetNamespace() + "/" + sa.GetName()

	var gtr capsulev1beta2.GlobalTenantResourceList
	if err := r.List(
		ctx,
		&gtr,
		client.MatchingFields{tenantresource.ServiceAccountIndexerFieldName: key},
	); err != nil {
		return false, err
	}

	if len(gtr.Items) > 0 {
		return true, nil
	}

	var ntr capsulev1beta2.TenantResourceList
	if err := r.List(
		ctx,
		&ntr,
		client.MatchingFields{tenantresource.ServiceAccountIndexerFieldName: key},
	); err != nil {
		return false, err
	}

	if len(ntr.Items) > 0 {
		return true, nil
	}

	return false, nil
}
