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

	"github.com/projectcapsule/capsule/internal/controllers/utils"
	"github.com/projectcapsule/capsule/pkg/cache"
	"github.com/projectcapsule/capsule/pkg/configuration"
)

type Manager struct {
	Client        client.Client
	Log           logr.Logger
	Configuration configuration.Configuration
	Cache         *cache.ImpersonationCache
}

func (r *Manager) SetupWithManager(mgr ctrl.Manager, ctrlConfig utils.ControllerOptions) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&corev1.ServiceAccount{}).
		Complete(r)
}

func (r *Manager) Reconcile(ctx context.Context, request reconcile.Request) (res reconcile.Result, err error) {
	instance := &corev1.ServiceAccount{}
	if err = r.Client.Get(ctx, request.NamespacedName, instance); err != nil && !apierrors.IsNotFound(err) {
		r.Log.Error(err, "Error reading the object")

		return res, err
	}

	if apierrors.IsNotFound(err) || !instance.ObjectMeta.DeletionTimestamp.IsZero() {
		r.Log.V(4).Info("invalidating cache for serviceaccount cache", "name", request.Name, "namespace", request.Namespace)

		r.Cache.Invalidate(request.Namespace, request.Name)
	}

	r.Log.V(5).Info("checking chache references for serviceaccount", "name", request.Name, "namespace", request.Namespace)

	r.Log.V(5).Info("client cache stats", "size", r.Cache.Stats())

	return reconcile.Result{
		Requeue:      true,
		RequeueAfter: r.Configuration.CacheInvalidation().Duration,
	}, nil
}
