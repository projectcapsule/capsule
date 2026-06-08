// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package tenant

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/api/rules"
	"github.com/projectcapsule/capsule/pkg/runtime/selectors"
)

func GetManagedRuleStatus(
	ctx context.Context,
	c client.Reader,
	ns *corev1.Namespace,
) (*capsulev1beta2.RuleStatus, error) {
	obj := &capsulev1beta2.RuleStatus{}

	err := c.Get(ctx, types.NamespacedName{Name: meta.NameForManagedRuleStatus(), Namespace: ns.GetName()}, obj)
	if err != nil {
		return nil, err
	}

	return obj, err
}

// BuildNamespaceRuleBodyForNamespace returns the aggregated rule body that applies to `ns`.
// - Rules with nil NamespaceSelector match all namespaces.
// - Matching rules are combined in the order they appear in tnt.Spec.Rules (important for "later wins" semantics).
func BuildNamespaceRuleBodyStatus(
	ctx context.Context,
	c client.Reader,
	ns *corev1.Namespace,
	tnt *capsulev1beta2.Tenant,
) ([]*rules.NamespaceRuleBodyNamespace, error) {
	if tnt == nil || ns == nil {
		return nil, nil
	}

	// Treat nil labels map as empty.
	nsLabels := labels.Set{}
	if ns.Labels != nil {
		nsLabels = labels.Set(ns.Labels)
	}

	out := make([]*rules.NamespaceRuleBodyNamespace, 0, len(tnt.Spec.Rules))

	for i, rule := range tnt.Spec.Rules {
		if rule == nil {
			continue
		}

		if rule.NamespaceSelector != nil {
			matches, err := selectors.MatchesSelector(nsLabels, *rule.NamespaceSelector)
			if err != nil {
				return nil, fmt.Errorf("invalid namespaceSelector in rules[%d]: %w", i, err)
			}

			if !matches {
				continue
			}
		}

		normalized := rules.NamespaceRuleBodyNamespace{
			Enforce: rules.NamespaceRuleEnforceBody{
				Action: rule.Enforce.Action,
				Registries: append(
					[]rules.OCIRegistry(nil),
					rule.Enforce.Registries...,
				),
			},
		}

		if normalized.Enforce.Action == "" {
			normalized.Enforce.Action = rules.ActionTypeDeny
		}
		if len(normalized.Enforce.Registries) == 0 {
			continue
		}

		out = append(out, &normalized)
	}

	return out, nil
}
