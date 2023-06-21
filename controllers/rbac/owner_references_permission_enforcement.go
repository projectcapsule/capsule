// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package rbac

import (
	"context"
	"fmt"

	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	capsulev1beta2 "github.com/clastix/capsule/api/v1beta2"
)

type OwnerReferencesPermissionEnforcement struct {
	Client client.Client
}

func (o OwnerReferencesPermissionEnforcement) resourceName(tnt *capsulev1beta2.Tenant) string {
	return fmt.Sprintf("capsule:%s:orpe", tnt.Name)
}

func (o OwnerReferencesPermissionEnforcement) ensureClusterRole(ctx context.Context, tnt *capsulev1beta2.Tenant) (controllerutil.OperationResult, error) {
	cr := rbacv1.ClusterRole{}
	cr.Name = o.resourceName(tnt)

	return controllerutil.CreateOrUpdate(ctx, o.Client, &cr, func() error {
		cr.Rules = []rbacv1.PolicyRule{
			{
				Verbs:         []string{"update"},
				APIGroups:     []string{capsulev1beta2.GroupVersion.Group},
				Resources:     []string{"tenants/finalizers"},
				ResourceNames: []string{tnt.Name},
			},
		}

		return ctrl.SetControllerReference(tnt, &cr, o.Client.Scheme())
	})
}

func (o OwnerReferencesPermissionEnforcement) ensureClusterRoleBinding(ctx context.Context, tnt *capsulev1beta2.Tenant) (controllerutil.OperationResult, error) {
	crb := rbacv1.ClusterRoleBinding{}
	crb.Name = o.resourceName(tnt)

	return controllerutil.CreateOrUpdate(ctx, o.Client, &crb, func() error {
		crb.Subjects = []rbacv1.Subject{}

		for _, owner := range tnt.Spec.Owners {
			crb.Subjects = append(crb.Subjects, rbacv1.Subject{
				APIGroup: rbacv1.GroupName,
				Kind:     owner.Kind.String(),
				Name:     owner.Name,
			})
		}

		crb.RoleRef = rbacv1.RoleRef{
			APIGroup: rbacv1.SchemeGroupVersion.Group,
			Kind:     "ClusterRole",
			Name:     o.resourceName(tnt),
		}

		return ctrl.SetControllerReference(tnt, &crb, o.Client.Scheme())
	})
}

func (o OwnerReferencesPermissionEnforcement) Reconcile(ctx context.Context, request reconcile.Request) (reconcile.Result, error) {
	logger := log.FromContext(ctx, "controller", "OwnerReferencesPermissionEnforcement")

	tnt := capsulev1beta2.Tenant{}
	if err := o.Client.Get(ctx, request.NamespacedName, &tnt); err != nil {
		if errors.IsNotFound(err) {
			logger.Info("object may have been deleted")

			return reconcile.Result{}, nil
		}

		logger.Error(err, "cannot retrieve the desired Tenant")

		return reconcile.Result{}, err
	}
	// ClusterRole
	res, err := o.ensureClusterRole(ctx, &tnt)
	if err != nil {
		logger.Error(err, "cannot create OwnerReferencesPermissionEnforcement ClusterRole")

		return reconcile.Result{}, err
	}

	logger.Info("ClusterRole reconciliation", "resource", "ClusterRole", "result", res)
	// ClusterRoleBinding
	res, err = o.ensureClusterRoleBinding(ctx, &tnt)
	if err != nil {
		logger.Error(err, "cannot create OwnerReferencesPermissionEnforcement ClusterRoleBinding")

		return reconcile.Result{}, err
	}

	logger.Info("reconciliation completed", "resource", "ClusterRoleBinding", "result", res)

	return reconcile.Result{}, nil
}

func (o OwnerReferencesPermissionEnforcement) SetupWithManager(mgr ctrl.Manager) (err error) {
	return ctrl.NewControllerManagedBy(mgr).
		For(&capsulev1beta2.Tenant{}).
		Owns(&rbacv1.ClusterRoleBinding{}).
		Owns(&rbacv1.ClusterRole{}).
		Complete(o)
}
