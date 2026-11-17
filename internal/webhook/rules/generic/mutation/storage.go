// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package mutation

import (
	"context"
	"fmt"
	"maps"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/projectcapsule/capsule/pkg/api/rules"
	"github.com/projectcapsule/capsule/pkg/ruleengine"
)

const attributeClassField = "volumeAttributesClassName"

func isClaim(gvk schema.GroupVersionKind) bool {
	return gvk.Group == corev1.GroupName && gvk.Kind == "PersistentVolumeClaim"
}

// mutateAttributeClassDefaults sets the class of a claim on creation. Merge
// fills an omitted or null class and keeps an empty one, which chooses no
// class; replace always sets the class. Entries apply in order, and the
// conditions of each one see the claim the entries before it left. The claim
// changes only once every entry succeeded. No VolumeAttributesClass is read.
func mutateAttributeClassDefaults(ctx context.Context, obj *unstructured.Unstructured, bodies []*rules.NamespaceRuleBodyNamespace, conditions *ruleengine.ConditionEvaluator) (bool, error) {
	if obj == nil || !hasAttributeClassDefaults(bodies) {
		return false, nil
	}

	spec, _ := obj.Object["spec"].(map[string]any)
	if raw, found := obj.Object["spec"]; found && raw != nil && spec == nil {
		return false, fmt.Errorf("spec must be an object")
	}

	current, set, err := claimClass(spec)
	if err != nil {
		return false, err
	}

	working := obj
	// The condition evaluator caches the object it decoded, which a change of
	// the class makes stale.
	stale := true

	for i, body := range bodies {
		if body == nil {
			continue
		}

		for j := range body.Mutate {
			mutation := &body.Mutate[j]

			class := mutation.Storage.AttributeClass
			if class == "" {
				continue
			}

			switch mutation.Action {
			case "", rules.MutationActionMerge:
				if set {
					continue
				}
			case rules.MutationActionReplace:
			default:
				return false, fmt.Errorf("rules[%d].mutate[%d].storage.attributeClass: unsupported mutation action %q", i, j, mutation.Action)
			}

			if len(mutation.Conditions) > 0 && stale {
				conditions.ResetObject()

				stale = false
			}

			matched, err := conditions.Matches(ctx, working, mutation.Conditions)
			if err != nil {
				return false, fmt.Errorf("rules[%d].mutate[%d]: %w", i, j, err)
			}

			if !matched || (set && current == class) {
				continue
			}

			if working == obj {
				spec = maps.Clone(spec)
				if spec == nil {
					spec = map[string]any{}
				}

				working = &unstructured.Unstructured{Object: maps.Clone(obj.Object)}
				working.Object["spec"] = spec
			}

			spec[attributeClassField] = class
			current, set, stale = class, true, true
		}
	}

	if working == obj {
		return false, nil
	}

	obj.Object = working.Object

	return true, nil
}

func hasAttributeClassDefaults(bodies []*rules.NamespaceRuleBodyNamespace) bool {
	for _, body := range bodies {
		if body == nil {
			continue
		}

		for j := range body.Mutate {
			if body.Mutate[j].Storage.AttributeClass != "" {
				return true
			}
		}
	}

	return false
}

// claimClass returns the class a claim names, and whether it names one: an
// empty name chooses no class, while an omitted or null one leaves the choice
// to defaults.
func claimClass(spec map[string]any) (string, bool, error) {
	value := spec[attributeClassField]
	if value == nil {
		return "", false, nil
	}

	name, ok := value.(string)
	if !ok {
		return "", false, fmt.Errorf("spec.%s must be a string", attributeClassField)
	}

	return name, true, nil
}
