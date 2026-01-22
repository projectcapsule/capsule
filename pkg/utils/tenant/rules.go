// Copyright 2020-2025 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package tenant

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api"
)

// BuildNamespaceRuleBodyForNamespace returns the aggregated rule body that applies to `ns`.
// - Rules with nil NamespaceSelector match all namespaces.
// - Matching rules are combined in the order they appear in tnt.Spec.Rules (important for "later wins" semantics).
func BuildNamespaceRuleBodyForNamespace(
	ns *corev1.Namespace,
	tnt *capsulev1beta2.Tenant,
) (*capsulev1beta2.NamespaceRuleBody, error) {
	out := &capsulev1beta2.NamespaceRuleBody{
		Enforce: capsulev1beta2.NamespaceRuleEnforceBody{
			Registries: make([]api.OCIRegistry, 0),
		},
	}

	if tnt == nil || ns == nil {
		return out, nil
	}

	// Treat nil labels map as empty.
	var nsLabels labels.Set
	if ns.Labels != nil {
		nsLabels = labels.Set(ns.Labels)
	} else {
		nsLabels = labels.Set{}
	}

	for i, rule := range tnt.Spec.Rules {
		if rule == nil {
			continue
		}

		matches, err := namespaceRuleMatches(nsLabels, rule.NamespaceSelector)
		if err != nil {
			return nil, fmt.Errorf("invalid namespaceSelector in rules[%d]: %w", i, err)
		}

		if !matches {
			continue
		}

		// Merge enforce body (for now: only registries)
		// Preserve order: append in the order rules are declared.
		if len(rule.Enforce.Registries) > 0 {
			out.Enforce.Registries = append(out.Enforce.Registries, rule.Enforce.Registries...)
		}
	}

	return out, nil
}

func namespaceRuleMatches(nsLabels labels.Set, sel *metav1.LabelSelector) (bool, error) {
	// nil selector => match all
	if sel == nil {
		return true, nil
	}

	s, err := metav1.LabelSelectorAsSelector(sel)
	if err != nil {
		return false, err
	}

	return s.Matches(nsLabels), nil
}
