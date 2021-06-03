// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package servicelabels

import (
	"github.com/go-logr/logr"
	discoveryv1beta1 "k8s.io/api/discovery/v1beta1"
	ctrl "sigs.k8s.io/controller-runtime"
)

type EndpointSlicesLabelsReconciler struct {
	abstractServiceLabelsReconciler

	Log          logr.Logger
	VersionMinor int
	VersionMajor int
}

func (r *EndpointSlicesLabelsReconciler) SetupWithManager(mgr ctrl.Manager) error {
	r.scheme = mgr.GetScheme()
	r.abstractServiceLabelsReconciler = abstractServiceLabelsReconciler{
		scheme: mgr.GetScheme(),
		log:    r.Log,
	}

	if r.VersionMajor == 1 && r.VersionMinor <= 16 {
		r.Log.Info("Skipping controller setup, as EndpointSlices are not supported on current kubernetes version", "VersionMajor", r.VersionMajor, "VersionMinor", r.VersionMinor)
		return nil
	}

	r.abstractServiceLabelsReconciler.obj = &discoveryv1beta1.EndpointSlice{}
	return ctrl.NewControllerManagedBy(mgr).
		For(r.obj, r.abstractServiceLabelsReconciler.forOptionPerInstanceName()).
		Complete(r)
}
