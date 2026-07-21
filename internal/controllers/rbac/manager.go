// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package rbac

import (
	"context"
	"errors"
	"time"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apiserver/pkg/authentication/serviceaccount"
	"k8s.io/client-go/util/retry"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/internal/controllers/utils"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/api/rbac"
	"github.com/projectcapsule/capsule/pkg/runtime/configuration"
	"github.com/projectcapsule/capsule/pkg/runtime/predicates"
)

const controllerManager = "rbac-controller"

const (
	rbacConfigurationEventMarker = "capsule-rbac-configuration"
	serviceAccountEventMarker    = "capsule-rbac-serviceaccount"
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
		Named("capsule/rbac/roles").
		For(&rbacv1.ClusterRole{}, namesPredicate).
		WithOptions(ctrlConfig.Runtime.ToControllerOptions()).
		Complete(r)
	if crErr != nil {
		err = errors.Join(err, crErr)
	}

	crbErr := ctrl.NewControllerManagedBy(mgr).
		Named("capsule/rbac/bindings").
		For(&rbacv1.ClusterRoleBinding{}, namesPredicate).
		Watches(
			&capsulev1beta2.CapsuleConfiguration{},
			handler.EnqueueRequestsFromMapFunc(r.enqueueRBACConfiguration),
			builder.WithPredicates(
				predicates.ProvisionerSubjectsChangedPredicate{},
				predicates.NamesMatchingPredicate{Names: []string{ctrlConfig.ConfigurationName}},
			),
		).
		WatchesMetadata(
			&corev1.ServiceAccount{},
			handler.EnqueueRequestsFromMapFunc(r.enqueueServiceAccountChange),
			builder.WithPredicates(predicates.PromotedServiceaccountPredicate{}),
		).
		WithOptions(ctrlConfig.Runtime.ToControllerOptions()).
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

	switch request.Namespace {
	case rbacConfigurationEventMarker:
		return res, r.reconcileConfiguration(ctx)
	case serviceAccountEventMarker:
		return res, r.EnsureClusterRoleBindingsProvisioner(ctx)
	}

	switch request.Name {
	case rbac.ProvisionerClusterRole:
		if err = r.EnsureClusterRoleProvisioner(ctx); err != nil {
			r.Log.Error(err, "reconciliation for ClusterRole failed", "ClusterRole", rbac.ProvisionerClusterRole)

			break
		}
	case rbac.DeleterClusterRole:
		if err = r.EnsureClusterRoleDeleter(ctx); err != nil {
			r.Log.Error(err, "reconciliation for ClusterRole failed", "ClusterRole", rbac.DeleterClusterRole)
		}
	}

	return res, err
}

func (r *Manager) EnsureClusterRoleBindingsProvisioner(ctx context.Context) error {
	log := log.FromContext(ctx)

	cfg := r.Configuration.RBAC()

	crb := &rbacv1.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{Name: cfg.ProvisionerClusterRole},
	}

	started := time.Now()
	defer logOperationDuration(log, "ensure provisioner ClusterRoleBinding", started)

	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		_, err := controllerutil.CreateOrUpdate(ctx, r.Client, crb, func() error {
			crb.RoleRef = rbacv1.RoleRef{
				Kind:     "ClusterRole",
				Name:     cfg.ProvisionerClusterRole,
				APIGroup: rbacv1.GroupName,
			}

			labels := crb.GetLabels()
			if labels == nil {
				labels = make(map[string]string)
			}

			labels[meta.CreatedByCapsuleLabel] = controllerManager

			crb.SetLabels(labels)

			crb.Subjects = nil

			users := r.Configuration.GetUsersByStatus()
			for _, u := range r.Configuration.Administrators() {
				users.Upsert(u)
			}

			for _, entity := range users {
				switch entity.Kind {
				case rbac.UserOwner:
					crb.Subjects = append(crb.Subjects, rbacv1.Subject{
						Kind: rbacv1.UserKind,
						Name: entity.Name,
					})
				case rbac.GroupOwner:
					crb.Subjects = append(crb.Subjects, rbacv1.Subject{
						Kind: rbacv1.GroupKind,
						Name: entity.Name,
					})
				case rbac.ServiceAccountOwner:
					namespace, name, err := serviceaccount.SplitUsername(entity.Name)
					if err != nil {
						log.Error(
							err,
							"can not parse serviceaccount reference",
							"subject", entity.Name,
							"kind", entity.Kind,
						)

						continue
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

				listStarted := time.Now()

				if err := r.Client.List(ctx, saList, client.MatchingLabels{
					meta.OwnerPromotionLabel: meta.ValueTrue,
				}); err != nil {
					logOperationDuration(log, "list promoted ServiceAccounts", listStarted)

					return err
				}

				logOperationDuration(log, "list promoted ServiceAccounts", listStarted)

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

		labels[meta.CreatedByCapsuleLabel] = controllerManager

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

	return nil
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

		labels[meta.CreatedByCapsuleLabel] = controllerManager

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
	if err != nil {
		return err
	}

	return nil
}

// Start is the Runnable function triggered upon Manager start-up to perform the first RBAC reconciliation
// since we're not creating empty CR and CRB upon Capsule installation: it's a run-once task, since the reconciliation
// is handled by the Reconciler implemented interface.
func (r *Manager) Start(ctx context.Context) error {
	return r.reconcileConfiguration(ctx)
}

func (r *Manager) enqueueServiceAccountChange(context.Context, client.Object) []reconcile.Request {
	if !r.Configuration.AllowServiceAccountPromotion() {
		return nil
	}

	return []reconcile.Request{{NamespacedName: client.ObjectKey{
		Name:      r.Configuration.RBAC().ProvisionerClusterRole,
		Namespace: serviceAccountEventMarker,
	}}}
}

func (r *Manager) enqueueRBACConfiguration(context.Context, client.Object) []reconcile.Request {
	return []reconcile.Request{{NamespacedName: client.ObjectKey{
		Name:      r.Configuration.RBAC().ProvisionerClusterRole,
		Namespace: rbacConfigurationEventMarker,
	}}}
}

func (r *Manager) reconcileConfiguration(ctx context.Context) error {
	if err := r.EnsureClusterRoleProvisioner(ctx); err != nil && !apierrors.IsAlreadyExists(err) {
		return err
	}

	if err := r.EnsureClusterRoleDeleter(ctx); err != nil && !apierrors.IsAlreadyExists(err) {
		return err
	}

	return r.garbageCollectRBAC(ctx)
}

func (r *Manager) garbageCollectRBAC(ctx context.Context) error {
	started := time.Now()
	defer logOperationDuration(log.FromContext(ctx), "garbage collect managed RBAC", started)

	rbac := r.Configuration.RBAC()

	desiredCR := map[string]struct{}{
		rbac.ProvisionerClusterRole: {},
		rbac.DeleterClusterRole:     {},
	}

	desiredCRB := map[string]struct{}{
		rbac.ProvisionerClusterRole: {},
	}

	if err := r.garbageCollectClusterRoles(ctx, desiredCR); err != nil {
		return err
	}

	if err := r.garbageCollectClusterRoleBindings(ctx, desiredCRB); err != nil {
		return err
	}

	return nil
}

func logOperationDuration(logger logr.Logger, operation string, started time.Time) {
	logger.V(4).Info("RBAC operation completed", "operation", operation, "duration", time.Since(started))
}

//nolint:dupl
func (r *Manager) garbageCollectClusterRoles(ctx context.Context, desired map[string]struct{}) error {
	list := &rbacv1.ClusterRoleList{}
	if err := r.Client.List(ctx, list, client.MatchingLabels{
		meta.CreatedByCapsuleLabel: controllerManager,
	}); err != nil {
		return err
	}

	for i := range list.Items {
		cr := &list.Items[i]
		if _, ok := desired[cr.Name]; ok {
			continue
		}

		if err := r.Client.Delete(ctx, cr); err != nil && !apierrors.IsNotFound(err) {
			return err
		}
	}

	return nil
}

//nolint:dupl
func (r *Manager) garbageCollectClusterRoleBindings(ctx context.Context, desired map[string]struct{}) error {
	list := &rbacv1.ClusterRoleBindingList{}
	if err := r.Client.List(ctx, list, client.MatchingLabels{
		meta.CreatedByCapsuleLabel: controllerManager,
	}); err != nil {
		return err
	}

	for i := range list.Items {
		crb := &list.Items[i]
		if _, ok := desired[crb.Name]; ok {
			continue
		}

		if err := r.Client.Delete(ctx, crb); err != nil && !apierrors.IsNotFound(err) {
			return err
		}
	}

	return nil
}
