// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package rbac

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
	"github.com/hashicorp/go-multierror"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/util/workqueue"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	capsulev1beta2 "github.com/clastix/capsule/api/v1beta2"
	"github.com/clastix/capsule/controllers/utils"
	"github.com/clastix/capsule/pkg/configuration"
)

type Manager struct {
	Log           logr.Logger
	Client        client.Client
	Configuration configuration.Configuration
}

func (r *Manager) SetupWithManager(ctx context.Context, mgr ctrl.Manager, configurationName string) (err error) {
	namesPredicate := utils.NamesMatchingPredicate(ProvisionerRoleName, DeleterRoleName)

	crErr := ctrl.NewControllerManagedBy(mgr).
		For(&rbacv1.ClusterRole{}, namesPredicate).
		Complete(r)
	if crErr != nil {
		err = multierror.Append(err, crErr)
	}

	crbErr := ctrl.NewControllerManagedBy(mgr).
		For(&rbacv1.ClusterRoleBinding{}, namesPredicate).
		Watches(&capsulev1beta2.CapsuleConfiguration{}, handler.Funcs{
			UpdateFunc: func(ctx context.Context, updateEvent event.UpdateEvent, limitingInterface workqueue.RateLimitingInterface) {
				if updateEvent.ObjectNew.GetName() == configurationName {
					if crbErr := r.EnsureClusterRoleBindings(ctx); crbErr != nil {
						r.Log.Error(err, "cannot update ClusterRoleBinding upon CapsuleConfiguration update")
					}
				}
			},
		}).
		Complete(r)

	if crbErr != nil {
		err = multierror.Append(err, crbErr)
	}

	return
}

// Reconcile serves both required ClusterRole and ClusterRoleBinding resources: that's ok, we're watching for multiple
// Resource kinds and we're just interested to the ones with the said name since they're bounded together.
func (r *Manager) Reconcile(ctx context.Context, request reconcile.Request) (res reconcile.Result, err error) {
	switch request.Name {
	case ProvisionerRoleName:
		if err = r.EnsureClusterRole(ctx, ProvisionerRoleName); err != nil {
			r.Log.Error(err, "Reconciliation for ClusterRole failed", "ClusterRole", ProvisionerRoleName)

			break
		}

		if err = r.EnsureClusterRoleBindings(ctx); err != nil {
			r.Log.Error(err, "Reconciliation for ClusterRoleBindings failed")

			break
		}
	case DeleterRoleName:
		if err = r.EnsureClusterRole(ctx, DeleterRoleName); err != nil {
			r.Log.Error(err, "Reconciliation for ClusterRole failed", "ClusterRole", DeleterRoleName)
		}
	}

	return
}

func (r *Manager) EnsureClusterRoleBindings(ctx context.Context) (err error) {
	crb := &rbacv1.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name: ProvisionerRoleName,
		},
	}

	_, err = controllerutil.CreateOrUpdate(ctx, r.Client, crb, func() (err error) {
		crb.RoleRef = provisionerClusterRoleBinding.RoleRef

		crb.Subjects = []rbacv1.Subject{}

		for _, group := range r.Configuration.UserGroups() {
			crb.Subjects = append(crb.Subjects, rbacv1.Subject{
				Kind: "Group",
				Name: group,
			})
		}

		return
	})

	return
}

func (r *Manager) EnsureClusterRole(ctx context.Context, roleName string) (err error) {
	role, ok := clusterRoles[roleName]
	if !ok {
		return fmt.Errorf("clusterRole %s is not mapped", roleName)
	}

	clusterRole := &rbacv1.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{
			Name: role.GetName(),
		},
	}

	_, err = controllerutil.CreateOrUpdate(ctx, r.Client, clusterRole, func() error {
		clusterRole.Rules = role.Rules

		return nil
	})

	return
}

// Start is the Runnable function triggered upon Manager start-up to perform the first RBAC reconciliation
// since we're not creating empty CR and CRB upon Capsule installation: it's a run-once task, since the reconciliation
// is handled by the Reconciler implemented interface.
func (r *Manager) Start(ctx context.Context) error {
	for roleName := range clusterRoles {
		r.Log.Info("setting up ClusterRoles", "ClusterRole", roleName)

		if err := r.EnsureClusterRole(ctx, roleName); err != nil {
			if apierrors.IsAlreadyExists(err) {
				continue
			}

			return err
		}
	}

	r.Log.Info("setting up ClusterRoleBindings")

	if err := r.EnsureClusterRoleBindings(ctx); err != nil {
		if apierrors.IsAlreadyExists(err) {
			return nil
		}

		return err
	}

	return nil
}
