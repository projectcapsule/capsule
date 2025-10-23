// Copyright 2020-2025 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package tenant

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/retry"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/meta"
	"github.com/projectcapsule/capsule/pkg/metrics"
)

type Manager struct {
	client.Client

	Metrics    *metrics.TenantRecorder
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

func (r Manager) Reconcile(ctx context.Context, request ctrl.Request) (result ctrl.Result, err error) {
	r.Log = r.Log.WithValues("Request.Name", request.Name)
	// Fetch the Tenant instance
	instance := &capsulev1beta2.Tenant{}
	if err = r.Get(ctx, request.NamespacedName, instance); err != nil {
		if apierrors.IsNotFound(err) {
			r.Log.V(3).Info("Request object not found, could have been deleted after reconcile request")

			// If tenant was deleted or cannot be found, clean up metrics
			r.Metrics.DeleteAllMetricsForTenant(request.Name)

			return reconcile.Result{}, nil
		}

		r.Log.Error(err, "Error reading the object")

		return result, err
	}

	defer func() {
		r.syncTenantStatusMetrics(instance)

		if uerr := r.updateTenantStatus(ctx, instance, err); uerr != nil {
			err = fmt.Errorf("cannot update tenant status: %w", uerr)

			return
		}
	}()

	// Ensuring Metadata.
	if err = r.ensureMetadata(ctx, instance); err != nil {
		err = fmt.Errorf("cannot ensure metadata: %w", err)

		return result, err
	}

	// Ensuring ResourceQuota
	r.Log.V(4).Info("Ensuring limit resources count is updated")

	if err = r.syncCustomResourceQuotaUsages(ctx, instance); err != nil {
		err = fmt.Errorf("cannot count limited resources: %w", err)

		return result, err
	}

	// Reconcile Namespaces
	r.Log.V(4).Info("Starting processing of Namespaces", "items", len(instance.Status.Namespaces))

	if err = r.reconcileNamespaces(ctx, instance); err != nil {
		err = fmt.Errorf("namespace(s) had reconciliation errors")

		return result, err
	}

	// Ensuring NetworkPolicy resources
	r.Log.V(4).Info("Starting processing of Network Policies")

	if err = r.syncNetworkPolicies(ctx, instance); err != nil {
		err = fmt.Errorf("cannot sync networkPolicy items: %w", err)

		return result, err
	}
	// Ensuring LimitRange resources
	r.Log.V(4).Info("Starting processing of Limit Ranges", "items", len(instance.Spec.LimitRanges.Items))

	if err = r.syncLimitRanges(ctx, instance); err != nil {
		err = fmt.Errorf("cannot sync limitrange items: %w", err)

		return result, err
	}
	// Ensuring ResourceQuota resources
	r.Log.V(4).Info("Starting processing of Resource Quotas", "items", len(instance.Spec.ResourceQuota.Items))

	if err = r.syncResourceQuotas(ctx, instance); err != nil {
		err = fmt.Errorf("cannot sync resourcequota items: %w", err)

		return result, err
	}
	// Ensuring RoleBinding resources
	r.Log.V(4).Info("Ensuring RoleBindings for Owners and Tenant")

	if err = r.syncRoleBindings(ctx, instance); err != nil {
		err = fmt.Errorf("cannot sync rolebindings items: %w", err)

		return result, err
	}

	r.Log.V(4).Info("Tenant reconciling completed")

	return ctrl.Result{}, err
}

func (r *Manager) updateTenantStatus(ctx context.Context, tnt *capsulev1beta2.Tenant, reconcileError error) error {
	return retry.RetryOnConflict(retry.DefaultBackoff, func() (err error) {
		latest := &capsulev1beta2.Tenant{}
		if err = r.Get(ctx, types.NamespacedName{Name: tnt.GetName()}, latest); err != nil {
			return err
		}

		latest.Status = tnt.Status

		// Set Ready Condition
		readyCondition := meta.NewReadyCondition(tnt)
		if reconcileError != nil {
			readyCondition.Message = reconcileError.Error()
			readyCondition.Status = metav1.ConditionFalse
			readyCondition.Reason = meta.FailedReason
		}

		latest.Status.Conditions.UpdateConditionByType(readyCondition)

		// Set Cordoned Condition
		cordonedCondition := meta.NewCordonedCondition(tnt)

		if tnt.Spec.Cordoned {
			latest.Status.State = capsulev1beta2.TenantStateCordoned

			cordonedCondition.Reason = meta.CordonedReason
			cordonedCondition.Message = "Tenant is cordoned"
			cordonedCondition.Status = metav1.ConditionTrue
		} else {
			latest.Status.State = capsulev1beta2.TenantStateActive
		}

		latest.Status.Conditions.UpdateConditionByType(cordonedCondition)

		return r.Client.Status().Update(ctx, latest)
	})
}
