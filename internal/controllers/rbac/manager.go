// Copyright 2020-2025 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package rbac

import (
	"context"
	"errors"

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
	"github.com/projectcapsule/capsule/pkg/runtime/configuration"
	"github.com/projectcapsule/capsule/pkg/runtime/predicates"
)

var (
	controllerManager = "rbac-controller"
)

type Manager struct {
	Log           logr.Logger
	Client        client.Client
	Configuration configuration.Configuration
}

//nolint:revive
func (r *Manager) SetupWithManager(ctx context.Context, mgr ctrl.Manager, ctrlConfig utils.ControllerOptions) (err error) {
	namesPredicate := predicates.LabelsMatching(map[string]string{
		meta.CreatedByCapsuleLabel: controllerManager,
	})

	crErr := ctrl.NewControllerManagedBy(mgr).
		Named("rbac/roles").
		For(&rbacv1.ClusterRole{}, namesPredicate).
		Complete(r)
	if crErr != nil {
		err = errors.Join(err, crErr)
	}

	crbErr := ctrl.NewControllerManagedBy(mgr).
		Named("rbac/bindings").
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
				if predicates.LabelsChanged([]string{meta.OwnerPromotionLabel}, e.ObjectOld.GetLabels(), e.ObjectNew.GetLabels()) {
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
	rbac := r.Configuration.RBAC()

	switch request.Name {
	case rbac.ProvisionerClusterRole:
		if err = r.EnsureClusterRoleProvisioner(ctx); err != nil {
			r.Log.Error(err, "Reconciliation for ClusterRole failed", "ClusterRole", rbac.ProvisionerClusterRole)

			break
		}
	case rbac.DeleterClusterRole:
		if err = r.EnsureClusterRoleDeleter(ctx); err != nil {
			r.Log.Error(err, "Reconciliation for ClusterRole failed", "ClusterRole", rbac.DeleterClusterRole)
		}
	}

	return res, err
}

func (r *Manager) EnsureClusterRoleBindingsProvisioner(ctx context.Context) error {
	rbac := r.Configuration.RBAC()

	crb := &rbacv1.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{Name: rbac.ProvisionerClusterRole},
	}

	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		_, err := controllerutil.CreateOrUpdate(ctx, r.Client, crb, func() error {
			crb.RoleRef = rbacv1.RoleRef{
				Kind:     "ClusterRole",
				Name:     rbac.ProvisionerClusterRole,
				APIGroup: rbacv1.GroupName,
			}

			labels := crb.GetLabels()
			if labels == nil {
				labels = make(map[string]string)
			}

			labels[meta.CreatedByCapsuleLabel] = "rbac-controller"

			crb.SetLabels(labels)

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

func (r *Manager) EnsureClusterRoleProvisioner(ctx context.Context) (err error) {
	clusterRole := &rbacv1.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{
			Name: r.Configuration.RBAC().ProvisionerClusterRole,
		},
	}

	_, err = controllerutil.CreateOrUpdate(ctx, r.Client, clusterRole, func() error {
		labels := clusterRole.GetLabels()
		if labels == nil {
			labels = make(map[string]string)
		}

		labels[meta.CreatedByCapsuleLabel] = "rbac-controller"

		clusterRole.SetLabels(labels)

		clusterRole.Rules = []rbacv1.PolicyRule{
			{
				APIGroups: []string{""},
				Resources: []string{"namespaces"},
				Verbs:     []string{"create", "patch"},
			},
		}

		return nil
	})
	if err != nil {
		return err
	}

	err = r.EnsureClusterRoleBindingsProvisioner(ctx)
	if err != nil && apierrors.IsAlreadyExists(err) {
		return nil
	}

	return err
}

func (r *Manager) EnsureClusterRoleDeleter(ctx context.Context) (err error) {
	clusterRole := &rbacv1.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{
			Name: r.Configuration.RBAC().DeleterClusterRole,
		},
	}

	_, err = controllerutil.CreateOrUpdate(ctx, r.Client, clusterRole, func() error {
		labels := clusterRole.GetLabels()
		if labels == nil {
			labels = make(map[string]string)
		}

		labels[meta.CreatedByCapsuleLabel] = "rbac-controller"

		clusterRole.SetLabels(labels)

		clusterRole.Rules = []rbacv1.PolicyRule{
			{
				APIGroups: []string{""},
				Resources: []string{"namespaces"},
				Verbs:     []string{"delete"},
			},
		}

		return nil
	})

	return err
}

// Start is the Runnable function triggered upon Manager start-up to perform the first RBAC reconciliation
// since we're not creating empty CR and CRB upon Capsule installation: it's a run-once task, since the reconciliation
// is handled by the Reconciler implemented interface.
func (r *Manager) Start(ctx context.Context) error {
	if err := r.EnsureClusterRoleProvisioner(ctx); err != nil && !apierrors.IsAlreadyExists(err) {
		return err
	}

	if err := r.EnsureClusterRoleDeleter(ctx); err != nil && !apierrors.IsAlreadyExists(err) {
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
