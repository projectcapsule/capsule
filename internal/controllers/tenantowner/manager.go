// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0
package tenantowners

import (
	"context"
	"fmt"
	"sort"

	"github.com/go-logr/logr"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/internal/controllers/utils"
	capmeta "github.com/projectcapsule/capsule/pkg/api/meta"
	indexer "github.com/projectcapsule/capsule/pkg/runtime/indexers/tenant"
	"github.com/projectcapsule/capsule/pkg/runtime/predicates"
	"github.com/projectcapsule/capsule/pkg/tenant"
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
			handler.EnqueueRequestsFromMapFunc(func(ctx context.Context, obj client.Object) []reconcile.Request {
				tnt, ok := obj.(*capsulev1beta2.Tenant)
				if !ok {
					return nil
				}

				requests := make([]reconcile.Request, 0, len(tnt.Status.Owners))

				for _, owner := range tnt.Status.Owners {
					requests = append(requests, reconcile.Request{
						NamespacedName: types.NamespacedName{
							Name: owner.Name,
						},
					})
				}

				return requests
			}),
			builder.WithPredicates(predicates.TenantStatusOwnersChangedPredicate{}),
		).
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
	if to.Spec.Name == "" || to.Spec.Kind.String() == "" {
		return nil, fmt.Errorf(
			"TenantOwner %s has incomplete owner reference: kind=%q name=%q",
			to.Name,
			to.Spec.Kind,
			to.Spec.Name,
		)
	}

	ownerKey := tenant.OwnerKindIndexKey(to.Spec.Kind.String(), to.Spec.Name)

	tnts := &capsulev1beta2.TenantList{}

	if err := r.List(
		ctx,
		tnts,
		client.MatchingFields{
			indexer.OwnerKindIndexerFieldName: ownerKey,
		},
	); err != nil {
		return nil, fmt.Errorf(
			"listing Tenants by owner %s/%s: %w",
			to.Spec.Kind,
			to.Spec.Name,
			err,
		)
	}

	matched := make([]string, 0, len(tnts.Items))

	for i := range tnts.Items {
		matched = append(matched, tnts.Items[i].Name)
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

		latest.Status.Tenants = matchedTenants

		readyCondition := capmeta.NewReadyCondition(latest)
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
