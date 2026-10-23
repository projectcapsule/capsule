// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package mutation

import (
	"context"
	"fmt"
	"maps"
	"reflect"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/utils/ptr"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	"github.com/projectcapsule/capsule/pkg/api/rules"
	"github.com/projectcapsule/capsule/pkg/ruleengine"
)

var listenerSetKind = schema.GroupVersionKind{Group: gatewayv1.GroupName, Version: gatewayv1.GroupVersion.Version, Kind: "ListenerSet"}

// mutateListenerSetParentRef applies ordered, audience-filtered entries on a
// main-resource ListenerSet CREATE or UPDATE. References are atomic. Only the
// changed spec projection is copied, and errors leave the input unchanged.
func mutateListenerSetParentRef(ctx context.Context, obj *unstructured.Unstructured, bodies []*rules.NamespaceRuleBodyNamespace, conditions *ruleengine.ConditionEvaluator) (bool, error) {
	if obj == nil {
		return false, nil
	}

	working := obj

	for i, body := range bodies {
		if body == nil {
			continue
		}

		for j := range body.Mutate {
			mutation := &body.Mutate[j]

			ref := mutation.Ingress.ListenerSets.ParentRef
			if ref == nil {
				continue
			}

			conditions.ResetObject()

			matched, err := conditions.Matches(ctx, working, mutation.Conditions)
			if err != nil {
				return false, fmt.Errorf("rules[%d].mutate[%d]: %w", i, j, err)
			}

			if !matched {
				continue
			}

			current, found, err := unstructured.NestedFieldNoCopy(working.Object, "spec", "parentRef")
			if err != nil {
				return false, fmt.Errorf("spec.parentRef: %w", err)
			}

			switch mutation.Action {
			case "", rules.MutationActionMerge:
				if found && current != nil {
					continue
				}
			case rules.MutationActionReplace:
			default:
				return false, fmt.Errorf("rules[%d].mutate[%d].action: unsupported mutation action %q", i, j, mutation.Action)
			}

			desired := map[string]any{"name": ref.Name, "group": ptr.Deref(ref.Group, gatewayv1.GroupName), "kind": ptr.Deref(ref.Kind, "Gateway")}
			if ref.Namespace != nil {
				desired["namespace"] = *ref.Namespace
			}

			if reflect.DeepEqual(current, desired) {
				continue
			}

			if working == obj {
				working = &unstructured.Unstructured{Object: maps.Clone(obj.Object)}
				spec, _ := obj.Object["spec"].(map[string]any)
				specCopy := make(map[string]any, len(spec)+1)
				maps.Copy(specCopy, spec)
				working.Object["spec"] = specCopy
			}

			if err := unstructured.SetNestedMap(working.Object, desired, "spec", "parentRef"); err != nil {
				return false, fmt.Errorf("set spec.parentRef: %w", err)
			}
		}
	}

	if working == obj {
		return false, nil
	}

	before, _, _ := unstructured.NestedFieldNoCopy(obj.Object, "spec", "parentRef")

	after, _, _ := unstructured.NestedFieldNoCopy(working.Object, "spec", "parentRef")
	if reflect.DeepEqual(before, after) {
		return false, nil
	}

	obj.Object = working.Object

	return true, nil
}
