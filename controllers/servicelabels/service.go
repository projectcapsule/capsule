// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package servicelabels

import (
	"context"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	ctrl "sigs.k8s.io/controller-runtime"
)

type ServicesLabelsReconciler struct {
	abstractServiceLabelsReconciler

	Log logr.Logger
}

func (r *ServicesLabelsReconciler) SetupWithManager(ctx context.Context, mgr ctrl.Manager) error {
	r.abstractServiceLabelsReconciler = abstractServiceLabelsReconciler{
		obj:    &corev1.Service{},
		client: mgr.GetClient(),
		log:    r.Log,
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(r.abstractServiceLabelsReconciler.obj, r.abstractServiceLabelsReconciler.forOptionPerInstanceName(ctx)).
		Complete(r)
}
