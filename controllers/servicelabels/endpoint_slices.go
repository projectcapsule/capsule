// Copyright 2020-2023 Project Capsule Authors.
// SPDX-License-Identifier: Apache-2.0

package servicelabels

import (
	"context"

	"github.com/go-logr/logr"
	discoveryv1 "k8s.io/api/discovery/v1"
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
		obj:    &discoveryv1.EndpointSlice{},
		client: mgr.GetClient(),
		log:    r.Log,
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(r.abstractServiceLabelsReconciler.obj, r.abstractServiceLabelsReconciler.forOptionPerInstanceName(ctx)).
		Complete(r)
}
