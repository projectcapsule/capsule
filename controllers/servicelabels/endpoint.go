// Copyright 2020-2023 Project Capsule Authors.
// SPDX-License-Identifier: Apache-2.0

package servicelabels

import (
	"context"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/controller"
)

type EndpointsLabelsReconciler struct {
	abstractServiceLabelsReconciler

	Log logr.Logger
}

func (r *EndpointsLabelsReconciler) SetupWithManager(ctx context.Context, mgr ctrl.Manager, workerCount int) error {
	r.abstractServiceLabelsReconciler = abstractServiceLabelsReconciler{
		obj:    &corev1.Endpoints{},
		client: mgr.GetClient(),
		log:    r.Log,
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(r.abstractServiceLabelsReconciler.obj, r.abstractServiceLabelsReconciler.forOptionPerInstanceName(ctx)).
		WithOptions(controller.Options{MaxConcurrentReconciles: workerCount}).
		Complete(r)
}
