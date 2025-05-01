// Copyright 2020-2023 Project Capsule Authors.
// SPDX-License-Identifier: Apache-2.0

package resourcepools

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api"
	"github.com/projectcapsule/capsule/pkg/meta"
	"github.com/projectcapsule/capsule/pkg/metrics"
)

type resourceClaimController struct {
	client.Client
	Metrics    *metrics.ClaimRecorder
	Log        logr.Logger
	Recorder   record.EventRecorder
	RESTConfig *rest.Config
}

func (r *resourceClaimController) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&capsulev1beta2.ResourcePoolClaim{}).
		Complete(r)
}

func (r resourceClaimController) Reconcile(ctx context.Context, request ctrl.Request) (result ctrl.Result, err error) {
	log := r.Log.WithValues("Request.Name", request.Name)

	instance := &capsulev1beta2.ResourcePoolClaim{}
	if err = r.Get(ctx, request.NamespacedName, instance); err != nil {
		if apierrors.IsNotFound(err) {
			log.Info("Request object not found, could have been deleted after reconcile request")

			r.Metrics.DeleteClaimMetric(request.Name)

			return reconcile.Result{}, nil
		}

		log.Error(err, "Error reading the object")

		return
	}

	// Ensuring the Quota Status
	err = r.reconcile(ctx, instance)

	// Emit a Metric in any case
	r.Metrics.RecordClaimCondition(instance)

	if err != nil {
		log.Error(err, "Cannot update Tenant status")

		return
	}

	return ctrl.Result{}, err
}

// This Controller is responsible for assigning Claims to ResourcePools.
// Everything else will be handeled by the ResourcePool Controller
func (r resourceClaimController) reconcile(
	ctx context.Context,
	claim *capsulev1beta2.ResourcePoolClaim,
) (err error) {
	pool, err := r.evaluateResourcePool(ctx, claim)
	if err != nil {
		claim.Status.Condition = meta.NewNotReadyCondition(claim, err.Error())
	}

	return r.allocateResourcePool(ctx, claim, pool)
}

// Verify a Pool can be allocated.
func (r resourceClaimController) evaluateResourcePool(
	ctx context.Context,
	claim *capsulev1beta2.ResourcePoolClaim,
) (pool *capsulev1beta2.ResourcePool, err error) {
	poolName := claim.Spec.Pool

	if claim.Status.Pool.Name != "" {
		poolName = claim.Status.Pool.Name.String()
	}

	if poolName == "" {
		err = fmt.Errorf("no pool reference was defined")

		return
	}

	pool = &capsulev1beta2.ResourcePool{}
	err = r.Get(ctx, client.ObjectKey{
		Name: poolName,
	}, pool)

	if err != nil {
		return
	}

	// Validates if Resources can be allocated in the first place
	for resourceName := range claim.Spec.ResourceClaims {
		_, exists := pool.Status.Allocation.Hard[resourceName]
		if !exists {
			return nil, fmt.Errorf(
				"Resource %s is not available in pool %s",
				resourceName,
				pool.Name,
			)
		}
	}

	return
}

func (r resourceClaimController) allocateResourcePool(
	ctx context.Context,
	claim *capsulev1beta2.ResourcePoolClaim,
	pool *capsulev1beta2.ResourcePool,
) (err error) {
	allocate := api.StatusNameUID{
		Name: api.Name(pool.GetName()),
		UID:  pool.GetUID(),
	}

	if claim.Status.Pool.Name == allocate.Name &&
		claim.Status.Pool.UID == allocate.UID {
		return nil
	}

	// Set claim pool in status and condition
	claim.Status = capsulev1beta2.ResourcePoolClaimStatus{
		Pool:      allocate,
		Condition: meta.NewQueuedReasonCondition(claim),
	}

	// Set metadata (owner ref)
	if _, err := controllerutil.CreateOrUpdate(ctx, r.Client, claim.DeepCopy(), func() error {
		return controllerutil.SetOwnerReference(pool, claim, r.Scheme())
	}); err != nil {
		return err
	}

	// Update status in a separate call
	if err := r.Client.Status().Update(ctx, claim); err != nil {
		return err
	}

	return nil
}
