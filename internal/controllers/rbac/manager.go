// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package rbac

import (
	"context"
	"errors"
	"fmt"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apiserver/pkg/authentication/serviceaccount"
	"k8s.io/client-go/util/retry"
	"k8s.io/client-go/util/workqueue"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/internal/controllers/utils"
	"github.com/projectcapsule/capsule/pkg/api"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/configuration"
)

type Manager struct {
	Log           logr.Logger
	Client        client.Client
	Configuration configuration.Configuration
}

//nolint:revive
func (r *Manager) SetupWithManager(ctx context.Context, mgr ctrl.Manager, ctrlConfig utils.ControllerOptions) (err error) {
	namesPredicate := utils.NamesMatchingPredicate(api.ProvisionerRoleName, api.DeleterRoleName)

	crErr := ctrl.NewControllerManagedBy(mgr).
		For(&rbacv1.ClusterRole{}, namesPredicate).
		Complete(r)
	if crErr != nil {
		err = errors.Join(err, crErr)
	}

	crbErr := ctrl.NewControllerManagedBy(mgr).
		For(&rbacv1.ClusterRoleBinding{}, namesPredicate).
		Watches(&capsulev1beta2.CapsuleConfiguration{}, handler.Funcs{
			UpdateFunc: func(ctx context.Context, updateEvent event.TypedUpdateEvent[client.Object], limitingInterface workqueue.TypedRateLimitingInterface[reconcile.Request]) {
				if updateEvent.ObjectNew.GetName() == ctrlConfig.ConfigurationName {
					if crbErr := r.EnsureClusterRoleBindingsProvisioner(ctx); crbErr != nil {
						r.Log.Error(err, "cannot update ClusterRoleBinding upon CapsuleConfiguration update")
					}
				}
			},
		}).
		Watches(&corev1.ServiceAccount{}, handler.Funcs{
			CreateFunc: func(ctx context.Context, e event.TypedCreateEvent[client.Object], q workqueue.TypedRateLimitingInterface[reconcile.Request]) {
				r.handleSAChange(ctx, e.Object)
			},
			UpdateFunc: func(ctx context.Context, e event.TypedUpdateEvent[client.Object], q workqueue.TypedRateLimitingInterface[reconcile.Request]) {
				if utils.LabelsChanged([]string{meta.OwnerPromotionLabel}, e.ObjectOld.GetLabels(), e.ObjectNew.GetLabels()) {
					r.handleSAChange(ctx, e.ObjectNew)
				}
			},
			DeleteFunc: func(ctx context.Context, e event.TypedDeleteEvent[client.Object], q workqueue.TypedRateLimitingInterface[reconcile.Request]) {
				r.handleSAChange(ctx, e.Object)
			},
		}).
		Complete(r)
	if crbErr != nil {
		err = errors.Join(err, crbErr)
	}

	return err
}

// Reconcile serves both required ClusterRole and ClusterRoleBinding resources: that's ok, we're watching for multiple
// Resource kinds and we're just interested to the ones with the said name since they're bounded together.
func (r *Manager) Reconcile(ctx context.Context, request reconcile.Request) (res reconcile.Result, err error) {
	switch request.Name {
	case api.ProvisionerRoleName:
		if err = r.EnsureClusterRole(ctx, api.ProvisionerRoleName); err != nil {
			r.Log.Error(err, "Reconciliation for ClusterRole failed", "ClusterRole", api.ProvisionerRoleName)

			break
		}

		if err = r.EnsureClusterRoleBindingsProvisioner(ctx); err != nil {
			r.Log.Error(err, "Reconciliation for ClusterRoleBindings (Provisioner) failed")

			break
		}
	case api.DeleterRoleName:
		if err = r.EnsureClusterRole(ctx, api.DeleterRoleName); err != nil {
			r.Log.Error(err, "Reconciliation for ClusterRole failed", "ClusterRole", api.DeleterRoleName)
		}
	}

	return res, err
}

func (r *Manager) EnsureClusterRoleBindingsProvisioner(ctx context.Context) error {
	crb := &rbacv1.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{Name: api.ProvisionerRoleName},
	}

	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		_, err := controllerutil.CreateOrUpdate(ctx, r.Client, crb, func() error {
			crb.RoleRef = api.ProvisionerClusterRoleBinding.RoleRef
			crb.Subjects = nil

			users := r.Configuration.GetUsersByStatus()
			for _, u := range r.Configuration.Administrators() {
				users.Upsert(u)
			}

			for _, entity := range users {
				switch entity.Kind {
				case api.UserOwner:
					crb.Subjects = append(crb.Subjects, rbacv1.Subject{
						Kind: rbacv1.UserKind,
						Name: entity.Name,
					})
				case api.GroupOwner:
					crb.Subjects = append(crb.Subjects, rbacv1.Subject{
						Kind: rbacv1.GroupKind,
						Name: entity.Name,
					})
				case api.ServiceAccountOwner:
					namespace, name, err := serviceaccount.SplitUsername(entity.Name)
					if err != nil {
						return err
					}

					crb.Subjects = append(crb.Subjects, rbacv1.Subject{
						Kind:      rbacv1.ServiceAccountKind,
						Name:      name,
						Namespace: namespace,
					})
				}
			}

			if r.Configuration.AllowServiceAccountPromotion() {
				saList := &corev1.ServiceAccountList{}
				if err := r.Client.List(ctx, saList, client.MatchingLabels{
					meta.OwnerPromotionLabel: meta.OwnerPromotionLabelTrigger,
				}); err != nil {
					return err
				}

				for _, sa := range saList.Items {
					crb.Subjects = append(crb.Subjects, rbacv1.Subject{
						Kind:      rbacv1.ServiceAccountKind,
						Name:      sa.Name,
						Namespace: sa.Namespace,
					})
				}
			}

			return nil
		})

		return err
	})
}

func (r *Manager) EnsureClusterRole(ctx context.Context, roleName string) (err error) {
	role, ok := api.ClusterRoles[roleName]
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

	return err
}

// Start is the Runnable function triggered upon Manager start-up to perform the first RBAC reconciliation
// since we're not creating empty CR and CRB upon Capsule installation: it's a run-once task, since the reconciliation
// is handled by the Reconciler implemented interface.
func (r *Manager) Start(ctx context.Context) error {
	for roleName := range api.ClusterRoles {
		r.Log.V(4).Info("setting up ClusterRoles", "ClusterRole", roleName)

		if err := r.EnsureClusterRole(ctx, roleName); err != nil {
			if apierrors.IsAlreadyExists(err) {
				continue
			}

			return err
		}
	}

	r.Log.V(4).Info("setting up ClusterRoleBindings")

	if err := r.EnsureClusterRoleBindingsProvisioner(ctx); err != nil {
		if apierrors.IsAlreadyExists(err) {
			return nil
		}

		return err
	}

	return nil
}

func (r *Manager) handleSAChange(ctx context.Context, obj client.Object) {
	if !r.Configuration.AllowServiceAccountPromotion() {
		return
	}

	if err := r.EnsureClusterRoleBindingsProvisioner(ctx); err != nil {
		r.Log.Error(err, "cannot update ClusterRoleBinding upon ServiceAccount event")
	}
}
