// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package tenant

import (
	"context"
	"fmt"
	"sort"

	"github.com/go-logr/logr"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/internal/controllers/utils"
	capmeta "github.com/projectcapsule/capsule/pkg/api/meta"
)

// TenantOwnerManager reconciles TenantOwner objects and keeps
// status.tenants / status.matchedTenants / status.conditions in sync.
//
// It watches both TenantOwner objects (for spec changes) and Tenant objects
// (for selector changes), re-enqueuing every TenantOwner whenever any Tenant
// changes, because a single Tenant change can affect any subset of TenantOwners.
type TenantOwnerManager struct {
	client.Client

	reader client.Reader

	Log logr.Logger
}

func (r *TenantOwnerManager) SetupWithManager(mgr ctrl.Manager, ctrlConfig utils.ControllerOptions) error {
	r.reader = mgr.GetAPIReader()

	// enqueueAllTenantOwners maps any Tenant event to a reconcile request for
	// every existing TenantOwner, because a matchOwners selector change can
	// affect any TenantOwner.
	enqueueAllTenantOwners := handler.EnqueueRequestsFromMapFunc(
		func(ctx context.Context, _ client.Object) []reconcile.Request {
			list := &capsulev1beta2.TenantOwnerList{}
			if err := r.List(ctx, list); err != nil {
				r.Log.Error(err, "cannot list TenantOwners for re-enqueue")

				return nil
			}

			requests := make([]reconcile.Request, len(list.Items))
			for i := range list.Items {
				requests[i] = reconcile.Request{
					NamespacedName: types.NamespacedName{Name: list.Items[i].Name},
				}
			}

			return requests
		},
	)

	return ctrl.NewControllerManagedBy(mgr).
		Named("capsule/tenant-owner-status").
		For(
			&capsulev1beta2.TenantOwner{},
			builder.WithPredicates(
				predicate.Or(
					predicate.GenerationChangedPredicate{},
					predicate.LabelChangedPredicate{},
				),
			),
		).
		Watches(
			&capsulev1beta2.Tenant{},
			enqueueAllTenantOwners,
			builder.WithPredicates(predicate.Or(
				predicate.GenerationChangedPredicate{},
				predicate.Funcs{DeleteFunc: func(event.DeleteEvent) bool { return true }},
			)),
		).
		WithOptions(controller.Options{MaxConcurrentReconciles: ctrlConfig.MaxConcurrentReconciles}).
		Complete(r)
}

func (r *TenantOwnerManager) Reconcile(ctx context.Context, req ctrl.Request) (result ctrl.Result, err error) {
	log := r.Log.WithValues("tenantowner", req.Name)

	instance := &capsulev1beta2.TenantOwner{}
	if err = r.Get(ctx, req.NamespacedName, instance); err != nil {
		if apierrors.IsNotFound(err) {
			return reconcile.Result{}, nil
		}

		return reconcile.Result{}, err
	}

	matchedTenants, reconcileErr := r.reconcileMatchedTenants(ctx, instance)

	log.V(5).Info("matched tenants", "count", len(matchedTenants))

	if statusErr := r.updateTenantOwnerStatus(ctx, instance, matchedTenants, reconcileErr); statusErr != nil {
		if apierrors.IsNotFound(statusErr) {
			return reconcile.Result{}, nil
		}

		return reconcile.Result{}, fmt.Errorf("cannot update TenantOwner status: %w", statusErr)
	}

	return reconcile.Result{}, reconcileErr
}

// reconcileMatchedTenants lists all Tenants and returns the sorted names of
// those whose spec.permissions.matchOwners selectors select this TenantOwner.
// All work is done against the in-memory cache — no direct API server calls.
func (r *TenantOwnerManager) reconcileMatchedTenants(ctx context.Context, to *capsulev1beta2.TenantOwner) ([]string, error) {
	tenantList := &capsulev1beta2.TenantList{}
	if err := r.List(ctx, tenantList, client.UnsafeDisableDeepCopy); err != nil {
		return nil, fmt.Errorf("listing Tenants: %w", err)
	}

	toLabels := labels.Set(to.Labels)

	matched := []string{}

	for i := range tenantList.Items {
		tnt := &tenantList.Items[i]

		for _, sel := range tnt.Spec.Permissions.MatchOwners {
			if sel == nil {
				continue
			}

			selector, err := metav1.LabelSelectorAsSelector(sel)
			if err != nil {
				return nil, fmt.Errorf("invalid matchOwners selector on Tenant %q: %w", tnt.Name, err)
			}

			if selector.Matches(toLabels) {
				matched = append(matched, tnt.Name)

				break
			}
		}
	}

	sort.Strings(matched)

	return matched, nil
}

// updateTenantOwnerStatus writes the reconciled status back to the API server
// using a RetryOnConflict re-GET loop to avoid clobbering concurrent updates.
func (r *TenantOwnerManager) updateTenantOwnerStatus(
	ctx context.Context,
	instance *capsulev1beta2.TenantOwner,
	matchedTenants []string,
	reconcileError error,
) error {
	return retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		latest := &capsulev1beta2.TenantOwner{}
		if err := r.reader.Get(ctx, types.NamespacedName{Name: instance.Name}, latest); err != nil {
			if apierrors.IsNotFound(err) {
				return nil
			}

			return err
		}

		latest.Status.ObservedGeneration = latest.GetGeneration()

		// Only overwrite match data on success; on error, preserve the last
		// known-good values so consumers see stable data while Ready=False.
		if reconcileError == nil {
			latest.Status.MatchedTenantNames = matchedTenants
			mt := int64(len(matchedTenants))
			latest.Status.MatchedTenants = &mt
		}

		readyCondition := capmeta.NewReadyCondition(latest)
		readyCondition.ObservedGeneration = latest.GetGeneration()

		if reconcileError != nil {
			readyCondition.Status = metav1.ConditionFalse
			readyCondition.Reason = capmeta.FailedReason
			readyCondition.Message = reconcileError.Error()
		}

		latest.Status.Conditions.UpdateConditionByType(readyCondition)

		if err := r.Client.Status().Update(ctx, latest); err != nil {
			if apierrors.IsNotFound(err) {
				return nil
			}

			return err
		}

		instance.Status = latest.Status

		return nil
	})
}
