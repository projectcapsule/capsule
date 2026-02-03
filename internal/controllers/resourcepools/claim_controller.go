// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package resourcepools

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
	gherrors "github.com/pkg/errors"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/events"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/cluster-api/util/patch"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/internal/controllers/utils"
	"github.com/projectcapsule/capsule/internal/metrics"
	"github.com/projectcapsule/capsule/pkg/api/meta"
)

type resourceClaimController struct {
	client.Client

	metrics  *metrics.ClaimRecorder
	log      logr.Logger
	recorder events.EventRecorder
}

func (r *resourceClaimController) SetupWithManager(mgr ctrl.Manager, cfg utils.ControllerOptions) error {
	return ctrl.NewControllerManagedBy(mgr).
		Named("capsule/resourcepools/claims").
		For(
			&capsulev1beta2.ResourcePoolClaim{},
			builder.WithPredicates(
				predicate.GenerationChangedPredicate{},
			),
		).
		Watches(
			&capsulev1beta2.ResourcePool{},
			handler.EnqueueRequestsFromMapFunc(r.claimsWithoutPoolFromNamespaces),
			builder.WithPredicates(predicate.ResourceVersionChangedPredicate{}),
		).
		WithOptions(controller.Options{MaxConcurrentReconciles: cfg.MaxConcurrentReconciles}).
		Complete(r)
}

func (r resourceClaimController) Reconcile(ctx context.Context, request ctrl.Request) (result ctrl.Result, err error) {
	log := r.log.WithValues("Request.Name", request.Name)

	instance := &capsulev1beta2.ResourcePoolClaim{}
	if err = r.Get(ctx, request.NamespacedName, instance); err != nil {
		if apierrors.IsNotFound(err) {
			log.V(3).Info("Request object not found, could have been deleted after reconcile request")

			r.metrics.DeleteClaimMetric(request.Name, request.Namespace)

			return reconcile.Result{}, nil
		}

		log.Error(err, "Error reading the object")

		return result, err
	}

	patchHelper, err := patch.NewHelper(instance, r.Client)
	if err != nil {
		return reconcile.Result{}, gherrors.Wrap(err, "failed to init patch helper")
	}

	defer func() {
		if uerr := r.updateStatus(ctx, instance, err); uerr != nil {
			err = uerr

			return
		}

		r.metrics.RecordClaimCondition(instance)

		if e := patchHelper.Patch(ctx, instance); e != nil {
			err = e

			return
		}

		err = nil
	}()

	err = r.reconcile(ctx, log, instance)

	return ctrl.Result{}, err
}

// Trigger claims from a namespace, which are not yet allocated.
// when a resourcepool updates it's status.
func (r *resourceClaimController) claimsWithoutPoolFromNamespaces(ctx context.Context, obj client.Object) []reconcile.Request {
	pool, ok := obj.(*capsulev1beta2.ResourcePool)
	if !ok {
		return nil
	}

	var requests []reconcile.Request

	for _, ns := range pool.Status.Namespaces {
		claimList := &capsulev1beta2.ResourcePoolClaimList{}
		if err := r.List(ctx, claimList, client.InNamespace(ns)); err != nil {
			r.log.Error(err, "Failed to list claims in namespace", "namespace", ns)

			continue
		}

		for _, claim := range claimList.Items {
			if claim.Status.Pool.UID == "" {
				requests = append(requests, reconcile.Request{
					NamespacedName: types.NamespacedName{
						Namespace: claim.Namespace,
						Name:      claim.Name,
					},
				})
			}
		}
	}

	return requests
}

// This Controller is responsible for assigning Claims to ResourcePools.
// Everything else will be handeled by the ResourcePool Controller.
func (r resourceClaimController) reconcile(
	ctx context.Context,
	log logr.Logger,
	claim *capsulev1beta2.ResourcePoolClaim,
) (err error) {
	pool, err := r.evaluateResourcePool(ctx, claim)
	if err != nil {
		return err
	}

	return r.allocateResourcePool(ctx, log, claim, pool)
}

// Verify a Pool can be allocated.
func (r resourceClaimController) evaluateResourcePool(
	ctx context.Context,
	claim *capsulev1beta2.ResourcePoolClaim,
) (pool *capsulev1beta2.ResourcePool, err error) {
	poolName := claim.Spec.Pool

	if poolName == "" {
		err = fmt.Errorf("no pool reference was defined")

		return pool, err
	}

	pool = &capsulev1beta2.ResourcePool{}
	if err := r.Get(ctx, client.ObjectKey{
		Name: poolName,
	}, pool); err != nil {
		return nil, err
	}

	if !pool.DeletionTimestamp.IsZero() {
		return nil, fmt.Errorf(
			"resourcepool not available",
		)
	}

	allowed := false

	for _, ns := range pool.Status.Namespaces {
		if ns == claim.GetNamespace() {
			allowed = true

			continue
		}
	}

	if !allowed {
		return nil, fmt.Errorf(
			"resourcepool not available",
		)
	}

	// Validates if Resources can be allocated in the first place
	for resourceName := range claim.Spec.ResourceClaims {
		_, exists := pool.Status.Allocation.Hard[resourceName]
		if !exists {
			return nil, fmt.Errorf(
				"resource %s is not available in pool %s",
				resourceName,
				pool.Name,
			)
		}
	}

	return pool, err
}

func (r resourceClaimController) allocateResourcePool(
	ctx context.Context,
	log logr.Logger,
	cl *capsulev1beta2.ResourcePoolClaim,
	pool *capsulev1beta2.ResourcePool,
) (err error) {
	target := meta.LocalRFC1123ObjectReferenceWithUID{
		Name: meta.RFC1123Name(pool.GetName()),
		UID:  pool.GetUID(),
	}

	if cl.Status.Pool.Name != "" || cl.Status.Pool.UID != "" {
		if cl.Status.Pool.Name != target.Name ||
			cl.Status.Pool.UID != target.UID {
			if cl.IsBoundInResourcePool() {
				return fmt.Errorf("can not change pool while claim is in use for pool %s", cl.Status.Pool.Name)
			}

			// Removes old pool reference
			meta.RemoveLooseOwnerReference(cl, metav1.OwnerReference{
				APIVersion: pool.APIVersion,
				Kind:       pool.Kind,
				Name:       string(cl.Status.Pool.Name),
				UID:        cl.Status.Pool.UID,
			})

			r.metrics.DeletePoolAssociation(cl.GetName(), cl.GetNamespace(), string(cl.Status.Pool.Name))
		}
	}

	ref := meta.GetLooseOwnerReference(pool)

	// Sanitize any previous References
	meta.RemoveLooseOwnerReferenceForKindExceptGiven(cl, ref)

	if !meta.HasLooseOwnerReference(cl, ref) {
		log.V(4).Info("adding ownerreference", "pool", pool.Name)

		if err := meta.SetLooseOwnerReference(cl, ref); err != nil {
			return err
		}
	}

	cl.Status.Pool = target

	return nil
}

func (r *resourceClaimController) updateStatus(
	ctx context.Context,
	instance *capsulev1beta2.ResourcePoolClaim,
	reconcileError error,
) error {
	return retry.RetryOnConflict(retry.DefaultBackoff, func() (err error) {
		latest := &capsulev1beta2.ResourcePoolClaim{}
		if err = r.Get(ctx, types.NamespacedName{Name: instance.GetName(), Namespace: instance.GetNamespace()}, latest); err != nil {
			return err
		}

		latest.Status = instance.Status

		// Set Ready Condition
		readyCondition := meta.NewReadyCondition(instance)
		if reconcileError != nil {
			readyCondition.Message = reconcileError.Error()
			readyCondition.Status = metav1.ConditionFalse
			readyCondition.Reason = meta.FailedReason
		}

		latest.Status.Conditions.UpdateConditionByType(readyCondition)

		// Unset legacy Status
		//nolint:staticcheck
		latest.Status.Condition = metav1.Condition{}

		return r.Client.Status().Update(ctx, latest)
	})
}
