// Copyright 2020-2025 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package tenant

import (
	"context"
	"fmt"

	"golang.org/x/sync/errgroup"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/utils"
)

// Sync the dynamic Tenant Owner specific cluster-roles and additional Role Bindings, which can be used in many ways:
// applying Pod Security Policies or giving access to CRDs or specific API groups.
func (r *Manager) syncRoleBindings(ctx context.Context, tenant *capsulev1beta2.Tenant) (err error) {
	roleBindings := tenant.GetRoleBindings()

	// Hashing
	hashes := map[string]api.AdditionalRoleBindingsSpec{}

	for _, binding := range roleBindings {
		hash := utils.RoleBindingHashFunc(binding)

		hashes[hash] = binding
	}

	group := new(errgroup.Group)

	for _, ns := range tenant.Status.Namespaces {
		namespace := ns

		group.Go(func() error {
			return r.syncAdditionalRoleBinding(ctx, tenant, namespace, hashes)
		})
	}

	return group.Wait()
}

func (r *Manager) syncAdditionalRoleBinding(
	ctx context.Context,
	tenant *capsulev1beta2.Tenant,
	ns string,
	bindings map[string]api.AdditionalRoleBindingsSpec,
) (err error) {
	keys := []string{}

	for hash, roleBinding := range bindings {
		name := meta.NameForManagedRoleBindings(hash)

		keys = append(keys, hash)

		target := &rbacv1.RoleBinding{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: ns,
			},
		}

		var res controllerutil.OperationResult

		res, err = controllerutil.CreateOrUpdate(ctx, r.Client, target, func() error {
			target.Labels = map[string]string{}
			target.Annotations = map[string]string{}

			if roleBinding.Labels != nil {
				target.Labels = roleBinding.Labels
			}

			target.Labels[meta.TenantLabel] = tenant.Name
			target.Labels[meta.RolebindingLabel] = hash

			if roleBinding.Annotations != nil {
				target.Annotations = roleBinding.Annotations
			}

			target.RoleRef = rbacv1.RoleRef{
				APIGroup: rbacv1.GroupName,
				Kind:     "ClusterRole",
				Name:     roleBinding.ClusterRoleName,
			}

			target.Subjects = roleBinding.Subjects

			return controllerutil.SetControllerReference(tenant, target, r.Scheme())
		})

		r.emitEvent(tenant, target.GetNamespace(), res, fmt.Sprintf("Ensuring RoleBinding %s", target.GetName()), err)

		if err != nil {
			r.Log.Error(err, "Cannot sync RoleBinding")
		}

		r.Log.V(4).Info(fmt.Sprintf("RoleBinding sync result: %s", string(res)), "name", target.Name, "namespace", target.Namespace)

		if err != nil {
			return err
		}
	}

	// Prune at finish to prevent gaps
	return r.pruningResources(ctx, ns, keys, &rbacv1.RoleBinding{})
}
