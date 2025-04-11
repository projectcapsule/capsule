// Copyright 2020-2023 Project Capsule Authors.
// SPDX-License-Identifier: Apache-2.0

package globalquota

import (
	"context"

	"github.com/go-logr/logr"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
)

type ResourceClaimController struct {
	client.Client
	Log        logr.Logger
	Recorder   record.EventRecorder
	RESTConfig *rest.Config
}

func (r *ResourceClaimController) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&capsulev1beta2.ResourceQuotaClaim{}).
		Complete(r)
}

//nolint:nakedret
func (r ResourceClaimController) Reconcile(ctx context.Context, request ctrl.Request) (result ctrl.Result, err error) {
	r.Log = r.Log.WithValues("Request.Name", request.Name)
	// Fetch the Tenant instance
	instance := &capsulev1beta2.ResourceQuotaClaim{}
	if err = r.Get(ctx, request.NamespacedName, instance); err != nil {
		if apierrors.IsNotFound(err) {
			r.Log.Info("Request object not found, could have been deleted after reconcile request")

			// Claim Status as Metrics

			return reconcile.Result{}, nil
		}

		r.Log.Error(err, "Error reading the object")

		return
	}

	// Ensuring the Quota Status
	if err = r.reconcile(ctx, instance); err != nil {
		r.Log.Error(err, "Cannot update Tenant status")

		return
	}

	return ctrl.Result{}, err
}

func (r ResourceClaimController) reconcile(
	ctx context.Context,
	claim *capsulev1beta2.ResourceQuotaClaim,
) (result ctrl.Result, err error) {
	r.Log = r.Log.WithValues(request.Name)
	// Fetch the Tenant instance
	instance := &capsulev1beta2.ResourceQuotaClaim{}
	if err = r.Get(ctx, request.NamespacedName, instance); err != nil {
		if apierrors.IsNotFound(err) {
			r.Log.Info("Request object not found, could have been deleted after reconcile request")

			// Claim Status as Metrics

			return reconcile.Result{}, nil
		}

		r.Log.Error(err, "Error reading the object")

		return
	}

	// Ensuring the Quota Status
	if err = r.reconcile(ctx, instance); err != nil {
		r.Log.Error(err, "Cannot update Tenant status")

		return
	}

	return ctrl.Result{}, err
}
