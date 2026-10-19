// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package tenant

import (
	"context"
	"fmt"
	"maps"
	"slices"

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

// namespaceRoleBindingPlan is immutable and shared by the namespace workers in
// one reconciliation. Hash bindings once, then select each namespace's profile
// from the same Namespace snapshot used to render its enforcement rules.
type namespaceRoleBindingPlan struct {
	common     map[string]rbac.AdditionalRoleBindingsSpec
	rules      []namespaceRoleBindingRule
	promotions map[string]map[string]rbac.AdditionalRoleBindingsSpec
}

type namespaceRoleBindingRule struct {
	index    int
	selector *metav1.LabelSelector
	bindings map[string]rbac.AdditionalRoleBindingsSpec
}

func prepareNamespaceRoleBindings(tenant *capsulev1beta2.Tenant) namespaceRoleBindingPlan {
	plan := namespaceRoleBindingPlan{
		common:     map[string]rbac.AdditionalRoleBindingsSpec{},
		promotions: map[string]map[string]rbac.AdditionalRoleBindingsSpec{},
	}

	for _, binding := range tenant.GetRoleBindings() {
		plan.common[utils.RoleBindingHashFunc(binding)] = binding
	}

	for i, rule := range tenant.Spec.Rules {
		if rule == nil || len(rule.Permissions.Bindings) == 0 {
			continue
		}

		bindings := make(map[string]rbac.AdditionalRoleBindingsSpec, len(rule.Permissions.Bindings))
		for _, binding := range rule.Permissions.Bindings {
			bindings[utils.RoleBindingHashFunc(binding)] = binding
		}

		plan.rules = append(plan.rules, namespaceRoleBindingRule{
			index: i, selector: rule.NamespaceSelector, bindings: bindings,
		})
	}

	for _, promotion := range tenant.GetPromotionRoleBindings() {
		namespace := string(promotion.Namespace)
		if plan.promotions[namespace] == nil {
			plan.promotions[namespace] = map[string]rbac.AdditionalRoleBindingsSpec{}
		}

		binding := promotion.AdditionalRoleBindingsSpec
		plan.promotions[namespace][utils.RoleBindingHashFunc(binding)] = binding
	}

	return plan
}

func (p namespaceRoleBindingPlan) forNamespace(ns *corev1.Namespace) (map[string]rbac.AdditionalRoleBindingsSpec, error) {
	bindings := maps.Clone(p.common)

	// Keep selector-based and selectorless rules in their original order: later rules
	// may override metadata on an otherwise identical binding.
	for _, rule := range p.rules {
		matches, err := utils.IsNamespaceSelectedBySelector(ns, rule.selector)
		if err != nil {
			return nil, fmt.Errorf("invalid namespaceSelector in rules[%d]: %w", rule.index, err)
		}

		if matches {
			maps.Copy(bindings, rule.bindings)
		}
	}

	maps.Copy(bindings, p.promotions[ns.Name])

	return bindings, nil
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
			Name:      name,
			Namespace: ns,
		}

		var result controllerutil.OperationResult

		result, err = controllerutil.CreateOrUpdate(ctx, r.Client, target, func() error {
			target.Labels = map[string]string{}
			target.Annotations = map[string]string{}

			maps.Copy(target.Labels, roleBinding.Labels)

			// Admission stamps both membership labels. Retain the same desired
			// labels so a steady-state sync does not rewrite every binding.
			target.Labels[meta.ManagedByCapsuleLabel] = tenant.Name
			target.Labels[meta.NewTenantLabel] = tenant.Name
			target.Labels[meta.RolebindingLabel] = hash
			target.Labels[meta.NewManagedByCapsuleLabel] = meta.ValueController

			// Remove Legacy labels
			delete(target.Labels, meta.TenantLabel)

			maps.Copy(target.Annotations, roleBinding.Annotations)

			target.RoleRef = rbacv1.RoleRef{
				APIGroup: rbacv1.GroupName,
				Kind:     "ClusterRole",
				Name:     roleBinding.ClusterRoleName,
			}

			// Client writes decode API responses into target. Keep that mutable
			// Subject slice separate from the immutable plan shared by workers.
			target.Subjects = slices.Clone(roleBinding.Subjects)
			for i, subject := range target.Subjects {
				if subject.APIGroup == "" && (subject.Kind == rbacv1.UserKind || subject.Kind == rbacv1.GroupKind) {
					target.Subjects[i].APIGroup = rbacv1.GroupName
				}
			}

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

		log.V(4).Info("RoleBinding sync result", "result", result, "name", target.Name, "namespace", target.Namespace)
	}

	// Prune at finish to prevent gaps
	return r.pruningResources(ctx, ns, keys, &rbacv1.RoleBinding{})
}
