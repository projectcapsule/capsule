// Copyright 2020-2025 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package resourcepools

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/retry"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api"
	"github.com/projectcapsule/capsule/pkg/meta"
	"github.com/projectcapsule/capsule/pkg/metrics"
)

type resourceClaimController struct {
	client.Client

	metrics  *metrics.ClaimRecorder
	log      logr.Logger
	recorder record.EventRecorder
}

func (r *resourceClaimController) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&capsulev1beta2.ResourcePoolClaim{}).
		Watches(
			&capsulev1beta2.ResourcePool{},
			handler.EnqueueRequestsFromMapFunc(r.claimsWithoutPoolFromNamespaces),
			builder.WithPredicates(predicate.ResourceVersionChangedPredicate{}),
		).
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

	// Ensuring the Quota Status
	err = r.reconcile(ctx, log, instance)

	// Emit a Metric in any case
	r.metrics.RecordClaimCondition(instance)

	if err != nil {
		return ctrl.Result{}, err
	}

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
		claim.Status.Pool = api.StatusNameUID{}

		cond := meta.NewAssignedCondition(claim)
		cond.Status = metav1.ConditionFalse
		cond.Reason = meta.FailedReason
		cond.Message = err.Error()

		return updateStatusAndEmitEvent(
			ctx,
			r.Client,
			r.recorder,
			claim,
			cond,
		)
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
	allocate := api.StatusNameUID{
		Name: api.Name(pool.GetName()),
		UID:  pool.GetUID(),
	}

	if !meta.HasLooseOwnerReference(cl, pool) {
		log.V(4).Info("adding ownerreference for", "pool", pool.Name)

		patch := client.MergeFrom(cl.DeepCopy())

		if err := meta.SetLooseOwnerReference(cl, pool, r.Scheme()); err != nil {
			return err
		}

		if err := r.Patch(ctx, cl, patch); err != nil {
			return err
		}
	}

	if cl.Status.Pool.Name == allocate.Name &&
		cl.Status.Pool.UID == allocate.UID {
		return nil
	}

	cond := meta.NewAssignedCondition(cl)
	cond.Status = metav1.ConditionTrue
	cond.Reason = meta.SucceededReason

	// Set claim pool in status and condition
	cl.Status = capsulev1beta2.ResourcePoolClaimStatus{
		Pool:      allocate,
		Condition: cond,
	}

	// Update status in a separate call
	if err := r.Client.Status().Update(ctx, cl); err != nil {
		return err
	}

	return nil
}

// Update the Status of a claim and emit an event if Status changed.
func updateStatusAndEmitEvent(
	ctx context.Context,
	c client.Client,
	recorder record.EventRecorder,
	claim *capsulev1beta2.ResourcePoolClaim,
	condition metav1.Condition,
) (err error) {
	if claim.Status.Condition.Type == condition.Type &&
		claim.Status.Condition.Status == condition.Status &&
		claim.Status.Condition.Reason == condition.Reason &&
		claim.Status.Condition.Message == condition.Message {
		return nil
	}

	err = retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		current := &capsulev1beta2.ResourcePoolClaim{}
		if err := c.Get(ctx, client.ObjectKeyFromObject(claim), current); err != nil {
			return fmt.Errorf("failed to refetch instance before update: %w", err)
		}

		current.Status.Condition = condition

		return c.Status().Update(ctx, current)
	})

	claim.Status.Condition = condition

	if err != nil {
		return err
	}

	eventType := corev1.EventTypeNormal
	if claim.Status.Condition.Status == metav1.ConditionFalse {
		eventType = corev1.EventTypeWarning
	}

	recorder.AnnotatedEventf(
		claim,
		map[string]string{
			"Status": string(claim.Status.Condition.Status),
			"Type":   claim.Status.Condition.Type,
		},
		eventType,
		claim.Status.Condition.Reason,
		claim.Status.Condition.Message,
	)

	return err
}
