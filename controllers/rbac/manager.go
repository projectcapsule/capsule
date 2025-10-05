// Copyright 2020-2025 Project Capsule Authors
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
	"k8s.io/client-go/util/retry"
	"k8s.io/client-go/util/workqueue"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/controllers/utils"
	"github.com/projectcapsule/capsule/pkg/configuration"
	"github.com/projectcapsule/capsule/pkg/meta"
)

type Manager struct {
	Log           logr.Logger
	Client        client.Client
	Configuration configuration.Configuration
}

//nolint:revive
func (r *Manager) SetupWithManager(ctx context.Context, mgr ctrl.Manager, configurationName string) (err error) {
	namesPredicate := utils.NamesMatchingPredicate(ProvisionerRoleName, DeleterRoleName)

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
				if updateEvent.ObjectNew.GetName() == configurationName {
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
				if promotionLabelsChanged(e.ObjectOld.GetLabels(), e.ObjectNew.GetLabels()) {
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
	case ProvisionerRoleName:
		if err = r.EnsureClusterRole(ctx, ProvisionerRoleName); err != nil {
			r.Log.Error(err, "Reconciliation for ClusterRole failed", "ClusterRole", ProvisionerRoleName)

			break
		}

		if err = r.EnsureClusterRoleBindingsProvisioner(ctx); err != nil {
			r.Log.Error(err, "Reconciliation for ClusterRoleBindings (Provisioner) failed")

			break
		}
	case DeleterRoleName:
		if err = r.EnsureClusterRole(ctx, DeleterRoleName); err != nil {
			r.Log.Error(err, "Reconciliation for ClusterRole failed", "ClusterRole", DeleterRoleName)
		}
	}

	return res, err
}

func (r *Manager) EnsureClusterRoleBindingsProvisioner(ctx context.Context) error {
	crb := &rbacv1.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{Name: ProvisionerRoleName},
	}

	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		_, err := controllerutil.CreateOrUpdate(ctx, r.Client, crb, func() error {
			crb.RoleRef = provisionerClusterRoleBinding.RoleRef
			crb.Subjects = nil

			for _, group := range r.Configuration.UserGroups() {
				crb.Subjects = append(crb.Subjects, rbacv1.Subject{
					Kind: rbacv1.GroupKind,
					Name: group,
				})
			}

			for _, user := range r.Configuration.UserNames() {
				crb.Subjects = append(crb.Subjects, rbacv1.Subject{
					Kind: rbacv1.UserKind,
					Name: user,
				})
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

	return err
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

func promotionLabelsChanged(oldLabels, newLabels map[string]string) bool {
	keys := []string{
		meta.OwnerPromotionLabel,
	}

	for _, key := range keys {
		oldVal, oldOK := oldLabels[key]
		newVal, newOK := newLabels[key]

		if oldOK != newOK || oldVal != newVal {
			return true
		}
	}

	return false
}
