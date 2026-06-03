// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package tenant

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/api/rbac"
	"github.com/projectcapsule/capsule/pkg/utils"
)

// Sync the dynamic Tenant Owner specific cluster-roles and additional Role Bindings, which can be used in many ways:
// applying Pod Security Policies or giving access to CRDs or specific API groups.
func (r *Manager) syncRoleBindings(ctx context.Context, log logr.Logger, tenant *capsulev1beta2.Tenant) (err error) {
	namespaceBindings := map[string]map[string]rbac.AdditionalRoleBindingsSpec{}

	for _, ns := range tenant.Status.Spaces {
		namespace := ns.Name

		if _, ok := namespaceBindings[namespace]; !ok {
			namespaceBindings[namespace] = map[string]rbac.AdditionalRoleBindingsSpec{}
		}
	}

	for _, binding := range tenant.GetRoleBindings() {
		hash := utils.RoleBindingHashFunc(binding)

		for namespace := range namespaceBindings {
			namespaceBindings[namespace][hash] = binding
		}
	}

	// Does not target all namespaces
	for _, promotion := range tenant.GetPromotionRoleBindings() {
		namespace := string(promotion.Namespace)

		if _, ok := namespaceBindings[namespace]; !ok {
			// Ignore namespaces that are not part of the tenant.
			continue
		}

		binding := promotion.AdditionalRoleBindingsSpec
		hash := utils.RoleBindingHashFunc(binding)

		namespaceBindings[namespace][hash] = binding
	}

	return runForTenantNamespaces(ctx, tenant, func(ctx context.Context, namespace string) error {
		return r.syncAdditionalRoleBinding(ctx, log, tenant, namespace, namespaceBindings[namespace])
	})
}

func (r *Manager) syncAdditionalRoleBinding(
	ctx context.Context,
	log logr.Logger,
	tenant *capsulev1beta2.Tenant,
	ns string,
	bindings map[string]rbac.AdditionalRoleBindingsSpec,
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

			target.Labels[meta.NewTenantLabel] = tenant.Name
			target.Labels[meta.RolebindingLabel] = hash
			target.Labels[meta.NewManagedByCapsuleLabel] = meta.ValueController

			// Remove Legacy labels
			delete(target.Labels, meta.TenantLabel)

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
		if err != nil {
			if apierrors.HasStatusCause(err, corev1.NamespaceTerminatingCause) {
				log.V(4).Info(
					"skipping RoleBinding sync because namespace is terminating",
					"name", target.Name,
					"namespace", target.Namespace,
					"clusterRole", roleBinding.ClusterRoleName,
				)

				continue
			}

			return fmt.Errorf("%w (role: %s)", err, roleBinding.ClusterRoleName)
		}

		log.V(4).Info(fmt.Sprintf("roleBinding sync result: %s", string(res)), "name", target.Name, "namespace", target.Namespace)
	}

	// Prune at finish to prevent gaps
	return r.pruningResources(ctx, ns, keys, &rbacv1.RoleBinding{})
}
