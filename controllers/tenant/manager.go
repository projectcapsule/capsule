// Copyright 2020-2023 Project Capsule Authors.
// SPDX-License-Identifier: Apache-2.0

package tenant

import (
	"context"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/retry"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/metrics"
)

type Manager struct {
	client.Client
	Log        logr.Logger
	Recorder   record.EventRecorder
	RESTConfig *rest.Config
}

func (r *Manager) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&capsulev1beta2.Tenant{}).
		Owns(&networkingv1.NetworkPolicy{}).
		Owns(&corev1.LimitRange{}).
		Owns(&corev1.ResourceQuota{}).
		Owns(&rbacv1.RoleBinding{}).
		Watches(&corev1.Namespace{}, handler.EnqueueRequestForOwner(mgr.GetScheme(), mgr.GetRESTMapper(), &capsulev1beta2.Tenant{})).
		Complete(r)
}

//nolint:nakedret
func (r Manager) Reconcile(ctx context.Context, request ctrl.Request) (result ctrl.Result, err error) {
	r.Log = r.Log.WithValues("Request.Name", request.Name)
	// Fetch the Tenant instance
	instance := &capsulev1beta2.Tenant{}
	if err = r.Get(ctx, request.NamespacedName, instance); err != nil {
		if apierrors.IsNotFound(err) {
			r.Log.Info("Request object not found, could have been deleted after reconcile request")

			// If tenant was deleted or cannot be found, clean up metrics
			metrics.TenantResourceUsage.DeletePartialMatch(map[string]string{"tenant": request.Name})
			metrics.TenantResourceLimit.DeletePartialMatch(map[string]string{"tenant": request.Name})

			return reconcile.Result{}, nil
		}

		r.Log.Error(err, "Error reading the object")

		return
	}
	// Ensuring the Tenant Status
	if err = r.updateTenantStatus(ctx, instance); err != nil {
		r.Log.Error(err, "Cannot update Tenant status")

		return
	}
	// Ensuring Metadata
	if err = r.ensureMetadata(ctx, instance); err != nil {
		r.Log.Error(err, "Cannot ensure metadata")

		return
	}

	// Ensuring ResourceQuota
	r.Log.Info("Ensuring limit resources count is updated")

	if err = r.syncCustomResourceQuotaUsages(ctx, instance); err != nil {
		r.Log.Error(err, "Cannot count limited resources")

		return
	}
	// Ensuring all namespaces are collected
	r.Log.Info("Ensuring all Namespaces are collected")

	if err = r.collectNamespaces(ctx, instance); err != nil {
		r.Log.Error(err, "Cannot collect Namespace resources")

		return
	}
	// Ensuring Namespace metadata
	r.Log.Info("Starting processing of Namespaces", "items", len(instance.Status.Namespaces))

	if err = r.syncNamespaces(ctx, instance); err != nil {
		r.Log.Error(err, "Cannot sync Namespace items")

		return
	}
	// Ensuring NetworkPolicy resources
	r.Log.Info("Starting processing of Network Policies")

	if err = r.syncNetworkPolicies(ctx, instance); err != nil {
		r.Log.Error(err, "Cannot sync NetworkPolicy items")

		return
	}
	// Ensuring LimitRange resources
	r.Log.Info("Starting processing of Limit Ranges", "items", len(instance.Spec.LimitRanges.Items))

	if err = r.syncLimitRanges(ctx, instance); err != nil {
		r.Log.Error(err, "Cannot sync LimitRange items")

		return
	}
	// Ensuring ResourceQuota resources
	r.Log.Info("Starting processing of Resource Quotas", "items", len(instance.Spec.ResourceQuota.Items))

	if err = r.syncResourceQuotas(ctx, instance); err != nil {
		r.Log.Error(err, "Cannot sync ResourceQuota items")

		return
	}
	// Ensuring RoleBinding resources
	r.Log.Info("Ensuring RoleBindings for Owners and Tenant")

	if err = r.syncRoleBindings(ctx, instance); err != nil {
		r.Log.Error(err, "Cannot sync RoleBindings items")

		return
	}
	// Ensuring Namespace count
	r.Log.Info("Ensuring Namespace count")

	if err = r.ensureNamespaceCount(ctx, instance); err != nil {
		r.Log.Error(err, "Cannot sync Namespace count")

		return
	}

	r.Log.Info("Tenant reconciling completed")

	return ctrl.Result{}, err
}

func (r *Manager) updateTenantStatus(ctx context.Context, tnt *capsulev1beta2.Tenant) error {
	return retry.RetryOnConflict(retry.DefaultBackoff, func() (err error) {
		if tnt.Spec.Cordoned {
			tnt.Status.State = capsulev1beta2.TenantStateCordoned
		} else {
			tnt.Status.State = capsulev1beta2.TenantStateActive
		}

		return r.Client.Status().Update(ctx, tnt)
	})
}
