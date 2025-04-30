// Copyright 2020-2023 Project Capsule Authors.
// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"context"

	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
)

// ResourceQuotaClaimReconciler reconciles a ResourceQuotaClaim object
type ResourceQuotaClaimReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=capsule.clastix.io,resources=resourcequotaclaims,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=capsule.clastix.io,resources=resourcequotaclaims/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=capsule.clastix.io,resources=resourcequotaclaims/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the ResourceQuotaClaim object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.20.4/pkg/reconcile
func (r *ResourceQuotaClaimReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	_ = logf.FromContext(ctx)

	// TODO(user): your logic here

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *ResourceQuotaClaimReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&capsulev1beta2.ResourceQuotaClaim{}).
		Named("resourcequotaclaim").
		Complete(r)
}
