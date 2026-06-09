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

// BuildNamespaceRuleBodyStatus returns the aggregated rule bodies that apply to ns.
// - Rules with nil NamespaceSelector match all namespaces.
// - Matching rules are returned in the order they appear in tnt.Spec.Rules.
// - Order is important because registry/QoS evaluation uses "later allow/deny wins" semantics.
func BuildNamespaceRuleBodyStatus(
	ctx context.Context,
	c client.Reader,
	ns *corev1.Namespace,
	tnt *capsulev1beta2.Tenant,
) ([]*rules.NamespaceRuleBodyNamespace, error) {
	if tnt == nil || ns == nil {
		return nil, nil
	}

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

		body := rule.NamespaceRuleBodyNamespace
		if body == nil || body.Enforce == nil {
			continue
		}

		out = append(out, body.DeepCopy())
	}

	return out, nil
}
