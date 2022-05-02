// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package servicelabels

import (
	"context"

	"github.com/go-logr/logr"
	discoveryv1 "k8s.io/api/discovery/v1"
	discoveryv1beta1 "k8s.io/api/discovery/v1beta1"
	ctrl "sigs.k8s.io/controller-runtime"
)

type EndpointSlicesLabelsReconciler struct {
	abstractServiceLabelsReconciler

	Log          logr.Logger
	VersionMinor uint
	VersionMajor uint
}

func (r *EndpointSlicesLabelsReconciler) SetupWithManager(ctx context.Context, mgr ctrl.Manager) error {
	r.abstractServiceLabelsReconciler = abstractServiceLabelsReconciler{
		log: r.Log,
	}

	switch {
	case r.VersionMajor == 1 && r.VersionMinor <= 16:
		r.Log.Info("Skipping controller setup, as EndpointSlices are not supported on current kubernetes version", "VersionMajor", r.VersionMajor, "VersionMinor", r.VersionMinor)

		return nil
	case r.VersionMajor == 1 && r.VersionMinor >= 21:
		r.abstractServiceLabelsReconciler.obj = &discoveryv1.EndpointSlice{}
	default:
		r.abstractServiceLabelsReconciler.obj = &discoveryv1beta1.EndpointSlice{}
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(r.obj, r.abstractServiceLabelsReconciler.forOptionPerInstanceName(ctx)).
		Complete(r)
}
