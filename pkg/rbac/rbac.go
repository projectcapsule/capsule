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

	"github.com/go-logr/logr"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

type Manager struct {
	client       client.Client
	CapsuleGroup string
	Log          logr.Logger
}

func (r *Manager) InjectClient(client client.Client) error {
	r.client = client
	return nil
}

func (r *Manager) Start(<-chan struct{}) (err error) {
	for roleName, role := range clusterRoles {
		r.Log.Info("setting up ClusterRoles", "ClusterRole", roleName)
		clusterRole := &rbacv1.ClusterRole{}
		if err := r.client.Get(context.TODO(), types.NamespacedName{Name: roleName}, clusterRole); err != nil {
			if errors.IsNotFound(err) {
				clusterRole.ObjectMeta = role.ObjectMeta
			}
		}
		_, err = controllerutil.CreateOrUpdate(context.TODO(), r.client, clusterRole, func() error {
			clusterRole.Rules = role.Rules
			return nil
		})
		if err != nil {
			return err
		}
	}
	clusterRoleBinding := &rbacv1.ClusterRoleBinding{}
	provisionerClusterRoleBinding.Subjects = []rbacv1.Subject{
		{
			Kind: "Group",
			Name: r.CapsuleGroup,
		},
	}
	r.Log.Info("setting up ClusterRoleBindings", "ClusterRoleBinding", ProvisionerRoleName)
	if err = r.client.Get(context.TODO(), types.NamespacedName{Name: ProvisionerRoleName}, clusterRoleBinding); err != nil {
		if errors.IsNotFound(err) {
			if err = r.client.Create(context.TODO(), provisionerClusterRoleBinding); err != nil {
				return err
			}
		}
	} else {
		// RoleRef is immutable, so we need to delete and recreate ClusterRoleBinding if it changed
		if !equality.Semantic.DeepDerivative(provisionerClusterRoleBinding.RoleRef, clusterRoleBinding.RoleRef) {
			if err = r.client.Delete(context.TODO(), clusterRoleBinding); err != nil {
				return err
			}
		}
		_, err = controllerutil.CreateOrUpdate(context.TODO(), r.client, clusterRoleBinding, func() error {
			clusterRoleBinding.Subjects = provisionerClusterRoleBinding.Subjects
			return nil
		})
		if err != nil {
			return err
		}
	}
	return nil
}
