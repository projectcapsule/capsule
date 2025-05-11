// Copyright 2020-2023 Project Capsule Authors.
// SPDX-License-Identifier: Apache-2.0

package tenant

import (
	"context"
	"fmt"
	"hash/fnv"
	"strings"

	"golang.org/x/sync/errgroup"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api"
	"github.com/projectcapsule/capsule/pkg/utils"
)

// ownerClusterRoleBindings generates a Capsule AdditionalRoleBinding object for the Owner dynamic clusterrole in order
// to take advantage of the additional role binding feature.
func (r *Manager) ownerClusterRoleBindings(owner capsulev1beta2.OwnerSpec, clusterRole string) api.AdditionalRoleBindingsSpec {
	var subject rbacv1.Subject

	if owner.Kind == "ServiceAccount" {
		splitName := strings.Split(owner.Name, ":")

		subject = rbacv1.Subject{
			Kind:      owner.Kind.String(),
			Name:      splitName[len(splitName)-1],
			Namespace: splitName[len(splitName)-2],
		}
	} else {
		subject = rbacv1.Subject{
			APIGroup: rbacv1.GroupName,
			Kind:     owner.Kind.String(),
			Name:     owner.Name,
		}
	}

	return api.AdditionalRoleBindingsSpec{
		ClusterRoleName: clusterRole,
		Subjects: []rbacv1.Subject{
			subject,
		},
	}
}

// Sync the dynamic Tenant Owner specific cluster-roles and additional Role Bindings, which can be used in many ways:
// applying Pod Security Policies or giving access to CRDs or specific API groups.
func (r *Manager) syncRoleBindings(ctx context.Context, tenant *capsulev1beta2.Tenant) (err error) {
	// hashing the RoleBinding name due to DNS RFC-1123 applied to Kubernetes labels
	hashFn := func(binding api.AdditionalRoleBindingsSpec) string {
		h := fnv.New64a()

		_, _ = h.Write([]byte(binding.ClusterRoleName))

		for _, sub := range binding.Subjects {
			_, _ = h.Write([]byte(sub.Kind + sub.Name))
		}

		return fmt.Sprintf("%x", h.Sum64())
	}
	// getting requested Role Binding keys
	keys := make([]string, 0, len(tenant.Spec.Owners))
	// Generating for dynamic tenant owners cluster roles
	for _, owner := range tenant.Spec.Owners {
		for _, clusterRoleName := range owner.ClusterRoles {
			cr := r.ownerClusterRoleBindings(owner, clusterRoleName)

			keys = append(keys, hashFn(cr))
		}
	}
	// Generating hash of additional role bindings
	for _, i := range tenant.Spec.AdditionalRoleBindings {
		keys = append(keys, hashFn(i))
	}

	group := new(errgroup.Group)

	for _, ns := range tenant.Status.Namespaces {
		namespace := ns

		group.Go(func() error {
			return r.syncAdditionalRoleBinding(ctx, tenant, namespace, keys, hashFn)
		})
	}

	return group.Wait()
}

//nolint:nakedret
func (r *Manager) syncAdditionalRoleBinding(ctx context.Context, tenant *capsulev1beta2.Tenant, ns string, keys []string, hashFn func(binding api.AdditionalRoleBindingsSpec) string) (err error) {
	var tenantLabel, roleBindingLabel string

	if tenantLabel, err = utils.GetTypeLabel(&capsulev1beta2.Tenant{}); err != nil {
		return
	}

	if roleBindingLabel, err = utils.GetTypeLabel(&rbacv1.RoleBinding{}); err != nil {
		return
	}

	if err = r.pruningResources(ctx, ns, keys, &rbacv1.RoleBinding{}); err != nil {
		return
	}

	var roleBindings []api.AdditionalRoleBindingsSpec

	for _, owner := range tenant.Spec.Owners {
		for _, clusterRoleName := range owner.ClusterRoles {
			roleBindings = append(roleBindings, r.ownerClusterRoleBindings(owner, clusterRoleName))
		}
	}

	roleBindings = append(roleBindings, tenant.Spec.AdditionalRoleBindings...)

	for i, roleBinding := range roleBindings {
		roleBindingHashLabel := hashFn(roleBinding)

		target := &rbacv1.RoleBinding{
			ObjectMeta: metav1.ObjectMeta{
				Name:      fmt.Sprintf("capsule-%s-%d-%s", tenant.Name, i, roleBinding.ClusterRoleName),
				Namespace: ns,
			},
		}

		var res controllerutil.OperationResult
		res, err = controllerutil.CreateOrUpdate(ctx, r.Client, target, func() error {
			if target.Labels == nil {
				target.Labels = map[string]string{}
			}

			target.Labels[tenantLabel] = tenant.Name
			target.Labels[roleBindingLabel] = roleBindingHashLabel
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

		r.Log.Info(fmt.Sprintf("RoleBinding sync result: %s", string(res)), "name", target.Name, "namespace", target.Namespace)

		if err != nil {
			return
		}
	}

	return nil
}

// ownerClusterRoleBindings generates a Capsule AdditionalRoleBinding object for the Owner dynamic clusterrole in order
// to take advantage of the additional role binding feature.
func (r *Manager) ownerClusterRoleBindingsToPermissions(owner capsulev1beta2.OwnerSpec, clusterRoles []string) capsulev1beta2.PermissionSpec {
	var subject capsulev1beta2.ExtendedSubject

	if owner.Kind == "ServiceAccount" {
		splitName := strings.Split(owner.Name, ":")

		subject = capsulev1beta2.ExtendedSubject{
			Subject: rbacv1.Subject{
				Kind:      owner.Kind.String(),
				Name:      splitName[len(splitName)-1],
				Namespace: splitName[len(splitName)-2],
			},
			// The owner should by default act as owner
			ActAsOwner: true,
		}
	} else {
		subject = capsulev1beta2.ExtendedSubject{
			Subject: rbacv1.Subject{
				APIGroup: rbacv1.GroupName,
				Kind:     owner.Kind.String(),
				Name:     owner.Name,
			},
			// The owner should by default act as owner
			ActAsOwner: true,
		}
	}

	return capsulev1beta2.PermissionSpec{
		RoleBindings: clusterRoles,
		Subjects: []capsulev1beta2.ExtendedSubject{
			subject,
		},
	}
}

// Sync the dynamic Permissions specific cluster-roles and role bindings.
func (r *Manager) syncPermissions(ctx context.Context, tenant *capsulev1beta2.Tenant) (err error) {

	// hashing the RoleBinding name due to DNS RFC-1123 applied to Kubernetes labels
	hashFn := func(binding capsulev1beta2.PermissionSpec) string {
		h := fnv.New64a()

		for _, cr := range binding.RoleBindings {
			_, _ = h.Write([]byte(cr))
		}

		for _, sub := range binding.Subjects {
			_, _ = h.Write([]byte(sub.Kind + sub.Name))
		}

		return fmt.Sprintf("%x", h.Sum64())
	}
	// getting requested Role Binding keys
	keys := make([]string, 0, len(tenant.Spec.Owners))
	// Generating for dynamic tenant owners cluster roles
	for _, owner := range tenant.Spec.Owners {
		cr := r.ownerClusterRoleBindingsToPermissions(owner, owner.ClusterRoles)

		keys = append(keys, hashFn(cr))
	}

	// Generating hash of additional role bindings
	for _, i := range tenant.Spec.Permissions {
		keys = append(keys, hashFn(i))
	}

	group := new(errgroup.Group)

	for _, ns := range tenant.Status.Namespaces {
		namespace := ns

		group.Go(func() error {
			return r.syncPermissionsRoleBindings(ctx, tenant, namespace, keys, hashFn)
		})
	}

	return group.Wait()
}

//nolint:nakedret
func (r *Manager) syncPermissionsRoleBindings(ctx context.Context, tenant *capsulev1beta2.Tenant, ns string, keys []string, hashFn func(binding capsulev1beta2.PermissionSpec) string) (err error) {

	var tenantLabel, roleBindingLabel string

	if tenantLabel, err = utils.GetTypeLabel(&capsulev1beta2.Tenant{}); err != nil {
		return
	}

	if roleBindingLabel, err = utils.GetTypeLabel(&rbacv1.RoleBinding{}); err != nil {
		return
	}

	if err = r.pruningResources(ctx, ns, keys, &rbacv1.RoleBinding{}); err != nil {
		return
	}

	var roleBindings []capsulev1beta2.PermissionSpec

	for _, owner := range tenant.Spec.Owners {
		roleBindings = append(roleBindings, r.ownerClusterRoleBindingsToPermissions(owner, owner.ClusterRoles))
	}

	for i, roleBinding := range roleBindings {
		roleBindingHashLabel := hashFn(roleBinding)

		for _, clusterRole := range roleBinding.RoleBindings {

			target := &rbacv1.RoleBinding{
				ObjectMeta: metav1.ObjectMeta{
					Name:      fmt.Sprintf("capsule-%s-%d-%s", tenant.Name, i, clusterRole),
					Namespace: ns,
				},
			}

			var res controllerutil.OperationResult
			res, err = controllerutil.CreateOrUpdate(ctx, r.Client, target, func() error {
				if target.Labels == nil {
					target.Labels = map[string]string{}
				}

				target.Labels[tenantLabel] = tenant.Name
				target.Labels[roleBindingLabel] = roleBindingHashLabel
				target.RoleRef = rbacv1.RoleRef{
					APIGroup: rbacv1.GroupName,
					Kind:     "ClusterRole",
					Name:     clusterRole,
				}

				// Extract rbacv1 Subjects from ExtendedSubjects
				subs := make([]rbacv1.Subject, len(roleBinding.Subjects))
				for i, extendedSubject := range roleBinding.Subjects {
					subs[i] = extendedSubject.Subject
				}
				target.Subjects = subs

				return controllerutil.SetControllerReference(tenant, target, r.Scheme())
			})

			r.emitEvent(tenant, target.GetNamespace(), res, fmt.Sprintf("Ensuring RoleBinding %s", target.GetName()), err)

			if err != nil {
				r.Log.Error(err, "Cannot sync RoleBinding")
			}

			r.Log.Info(fmt.Sprintf("RoleBinding sync result: %s", string(res)), "name", target.Name, "namespace", target.Namespace)

			if err != nil {
				return
			}
		}
	}

	return nil
}
