// Copyright 2020-2023 Project Capsule Authors.
// SPDX-License-Identifier: Apache-2.0

package tenantpermission

import (
	"context"

	"k8s.io/apimachinery/pkg/runtime"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
)

// TenantPermissionReconciler reconciles a TenantPermission object
type TenantPermissionReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// SetupWithManager sets up the controller with the Manager.
func (r *TenantPermissionReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&capsulev1beta2.TenantPermission{}).
		Complete(r)
}

// +kubebuilder:rbac:groups=capsule.clastix.io,resources=tenantpermissions,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=capsule.clastix.io,resources=tenantpermissions/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=capsule.clastix.io,resources=tenantpermissions/finalizers,verbs=update
func (r *TenantPermissionReconciler) Reconcile(ctx context.Context, req ctrl.Request) (result ctrl.Result, err error) {
	_ = log.FromContext(ctx)

	// TODO(user): your logic here

	tntPermission := &capsulev1beta2.TenantPermission{}
	if err := r.client.Get(ctx, req.NamespacedName, tntPermission); err != nil {
		if apierrors.IsNotFound(err) != nil {
			log.Info("Request object not found, could have been deleted after reconcile request")

			return ctrl.Result{}, err
		}
		
		return reconcile.Result{}, err
	}

	return ctrl.Result{}, nil
}
