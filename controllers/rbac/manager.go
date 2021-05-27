/*
Copyright 2020 Clastix Labs.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package rbac

import (
	"context"
	"fmt"
	"strings"

	b64 "encoding/base64"

	"github.com/go-logr/logr"
	"github.com/hashicorp/go-multierror"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const (
	rbacLabel = "capsule.clastix.io/rbac"
)

type Manager struct {
	CapsuleGroups []string
	Log           logr.Logger
	Client        client.Client
}

// Using the Client interface, required by the Runnable interface
func (r *Manager) InjectClient(c client.Client) error {
	r.Client = c
	return nil
}

func (r *Manager) filterByNames(name string) bool {
	return name == ProvisionerRoleName || name == DeleterRoleName
}

func (r *Manager) filterByLabels(labels map[string]string) bool {
	_, ok := labels[rbacLabel]
	return ok
}

//nolint:dupl
func (r *Manager) SetupWithManager(mgr ctrl.Manager) (err error) {
	crErr := ctrl.NewControllerManagedBy(mgr).
		For(&rbacv1.ClusterRole{}, builder.WithPredicates(predicate.Funcs{
			CreateFunc: func(event event.CreateEvent) bool {
				return r.filterByNames(event.Object.GetName())
			},
			DeleteFunc: func(deleteEvent event.DeleteEvent) bool {
				return r.filterByNames(deleteEvent.Object.GetName())
			},
			UpdateFunc: func(updateEvent event.UpdateEvent) bool {
				return r.filterByNames(updateEvent.ObjectNew.GetName())
			},
			GenericFunc: func(genericEvent event.GenericEvent) bool {
				return r.filterByNames(genericEvent.Object.GetName())
			},
		})).
		Complete(r)
	if crErr != nil {
		err = multierror.Append(err, crErr)
	}
	crbErr := ctrl.NewControllerManagedBy(mgr).
		For(&rbacv1.ClusterRoleBinding{}, builder.WithPredicates(predicate.Funcs{
			CreateFunc: func(event event.CreateEvent) bool {
				return r.filterByLabels(event.Object.GetLabels())
			},
			DeleteFunc: func(deleteEvent event.DeleteEvent) bool {
				return r.filterByLabels(deleteEvent.Object.GetLabels())
			},
			UpdateFunc: func(updateEvent event.UpdateEvent) bool {
				return r.filterByLabels(updateEvent.ObjectNew.GetLabels())
			},
			GenericFunc: func(genericEvent event.GenericEvent) bool {
				return r.filterByLabels(genericEvent.Object.GetLabels())
			},
		})).
		Complete(r)
	if crbErr != nil {
		err = multierror.Append(err, crbErr)
	}
	return
}

// This reconcile function is serving both ClusterRole and ClusterRoleBinding: that's ok, we're watching for multiple
// Resource kinds and we're just interested to the ones with the said name since they're bounded together.
func (r *Manager) Reconcile(ctx context.Context, request reconcile.Request) (res reconcile.Result, err error) {
	if strings.HasPrefix(request.Name, ProvisionerRoleName) {
		if err = r.EnsureClusterRole(ProvisionerRoleName); err != nil {
			r.Log.Error(err, "Reconciliation for ClusterRole failed", "ClusterRole", ProvisionerRoleName)
		}
		if err = r.EnsureClusterRoleBindings(); err != nil {
			r.Log.Error(err, "Reconciliation for ClusterRoleBindings failed")
		}
		return reconcile.Result{}, err
	}

	if err = r.EnsureClusterRole(DeleterRoleName); err != nil {
		r.Log.Error(err, "Reconciliation for ClusterRole failed", "ClusterRole", DeleterRoleName)
	}
	return reconcile.Result{}, err
}

func (r *Manager) EnsureClusterRoleBindings() (res error) {
	for _, group := range r.CapsuleGroups {
		name := fmt.Sprintf("%s-%v", ProvisionerRoleName, b64.RawStdEncoding.EncodeToString([]byte(group)))
		r.Log.Info("reconciling ClusterRoleBinding", "name", name, "capsule-user-group", group)

		crb := &rbacv1.ClusterRoleBinding{
			ObjectMeta: v1.ObjectMeta{
				Name: name,
				Labels: map[string]string{
					rbacLabel: name,
				},
			},
		}

		_, err := controllerutil.CreateOrUpdate(context.TODO(), r.Client, crb, func() error {
			// RoleRef is immutable, so we need to delete and recreate ClusterRoleBinding if it changed
			if crb.ResourceVersion != "" && !equality.Semantic.DeepDerivative(provisionerClusterRoleBinding.RoleRef, crb.RoleRef) {
				return ImmutableClusterRoleBindingError{}
			}
			crb.RoleRef = provisionerClusterRoleBinding.RoleRef
			crb.Subjects = []rbacv1.Subject{
				{
					Kind: "Group",
					Name: group,
				},
			}
			return nil
		})
		if err != nil {
			if _, ok := err.(ImmutableClusterRoleBindingError); ok {
				if err = r.Client.Delete(context.TODO(), crb); err != nil {
					r.Log.Error(err, "Cannot delete CRB during reset due to RoleRef change", "name", name, "capsule-user-group", group)
					res = multierror.Append(res, err)
					continue
				}
				if err = r.Client.Create(context.TODO(), provisionerClusterRoleBinding, &client.CreateOptions{}); err != nil {
					r.Log.Error(err, "Cannot create CRB during reset due to RoleRef change", "name", name, "capsule-user-group", group)
					res = multierror.Append(res, err)
					continue
				}
			}
			r.Log.Error(err, "Cannot CreateOrUpdate CRB", "name", name, "capsule-user-group", group)
			res = multierror.Append(res, err)
			continue
		}
	}
	return
}

func (r *Manager) EnsureClusterRole(roleName string) (err error) {
	role, ok := clusterRoles[roleName]
	if !ok {
		return fmt.Errorf("clusterRole %s is not mapped", roleName)
	}
	clusterRole := &rbacv1.ClusterRole{
		ObjectMeta: v1.ObjectMeta{
			Name: role.GetName(),
			Labels: map[string]string{
				rbacLabel: roleName,
			},
		},
	}
	_, err = controllerutil.CreateOrUpdate(context.TODO(), r.Client, clusterRole, func() error {
		clusterRole.Rules = role.Rules
		return nil
	})
	return
}

// This is the Runnable function that is triggered upon Manager start-up to perform the first RBAC reconciliation
// since we're not creating empty CR and CRB upon Capsule installation: it's a run-once task, since the reconciliation
// is handled by the Reconciler implemented interface.
func (r *Manager) Start(ctx context.Context) (err error) {
	r.Log.Info("cleaning up ClusterRoleBindings")
	if err = r.Client.DeleteAllOf(context.TODO(), &rbacv1.ClusterRoleBinding{}, client.HasLabels{rbacLabel}); err != nil {
		return
	}
	for roleName := range clusterRoles {
		r.Log.Info("setting up ClusterRoles", "ClusterRole", roleName)
		if err = r.EnsureClusterRole(roleName); err != nil {
			return
		}
	}
	r.Log.Info("setting up ClusterRoleBindings")
	return r.EnsureClusterRoleBindings()
}
