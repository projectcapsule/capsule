// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package servicelabels

import (
	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	ctrl "sigs.k8s.io/controller-runtime"
)

type ServicesLabelsReconciler struct {
	abstractServiceLabelsReconciler

	Log logr.Logger
}

func (r *ServicesLabelsReconciler) SetupWithManager(mgr ctrl.Manager) error {
	r.abstractServiceLabelsReconciler = abstractServiceLabelsReconciler{
		obj:    &corev1.Service{},
		scheme: mgr.GetScheme(),
		log:    r.Log,
	}
	return ctrl.NewControllerManagedBy(mgr).
		For(r.abstractServiceLabelsReconciler.obj, r.abstractServiceLabelsReconciler.forOptionPerInstanceName()).
		Complete(r)
}
