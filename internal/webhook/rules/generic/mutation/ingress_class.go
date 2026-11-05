// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package mutation

import (
	"context"
	"fmt"
	"maps"
	"slices"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/projectcapsule/capsule/internal/webhook/ingress"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/api/rules"
	"github.com/projectcapsule/capsule/pkg/ruleengine"
)

func mutateIngressClass(ctx context.Context, obj *unstructured.Unstructured, kind rules.IngressType, bodies []*rules.NamespaceRuleBodyNamespace, conditions *ruleengine.ConditionEvaluator) (bool, error) {
	if obj == nil || (kind != rules.IngressTypeIngress && kind != rules.IngressTypeGateway) {
		return false, nil
	}

	working := obj

	var original ingress.ClassReferences

	for i, body := range bodies {
		if body == nil {
			continue
		}

		for j := range body.Mutate {
			mutation := &body.Mutate[j]
			if mutation.Ingress.Class == "" || !slices.Contains(mutation.Ingress.Types, kind) {
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

			refs, err := ingress.ReadClassReferences(working, kind)
			if err != nil {
				return false, err
			}

			switch mutation.Action {
			case "", rules.MutationActionMerge:
				if refs.Name != "" || refs.Annotation != "" {
					continue
				}
			case rules.MutationActionReplace:
			default:
				return false, fmt.Errorf("rules[%d].mutate[%d].action: unsupported mutation action %q", i, j, mutation.Action)
			}

			name := mutation.Ingress.Class
			if refs.Name == name && (!refs.HasAnnotation || refs.Annotation == name) {
				continue
			}

			if working == obj {
				original = refs
				working = copyIngressClassObject(obj, refs.HasAnnotation)
			}

			if err := unstructured.SetNestedField(working.Object, name, "spec", refs.Field); err != nil {
				return false, err
			}

			if refs.HasAnnotation {
				if err := unstructured.SetNestedField(working.Object, name, "metadata", "annotations", meta.IngressClassAnnotation); err != nil {
					return false, err
				}
			}
		}
	}

	if working == obj {
		return false, nil
	}

	after, err := ingress.ReadClassReferences(working, kind)
	if err != nil || after == original {
		return false, err
	}

	obj.Object = working.Object

	return true, nil
}

// Copy only the projections that class mutation can change.
func copyIngressClassObject(obj *unstructured.Unstructured, annotation bool) *unstructured.Unstructured {
	working := &unstructured.Unstructured{Object: maps.Clone(obj.Object)}
	spec, _ := obj.Object["spec"].(map[string]any)
	specCopy := make(map[string]any, len(spec)+1)
	maps.Copy(specCopy, spec)

	working.Object["spec"] = specCopy

	if annotation {
		metadata, _ := obj.Object["metadata"].(map[string]any)
		metadataCopy := make(map[string]any, len(metadata))
		maps.Copy(metadataCopy, metadata)
		annotations, _ := metadata["annotations"].(map[string]any)
		annotationCopy := make(map[string]any, len(annotations))
		maps.Copy(annotationCopy, annotations)
		metadataCopy["annotations"] = annotationCopy
		working.Object["metadata"] = metadataCopy
	}

	return working
}

func mutateIngressRules(ctx context.Context, obj *unstructured.Unstructured, gvk schema.GroupVersionKind, bodies []*rules.NamespaceRuleBodyNamespace, conditions *ruleengine.ConditionEvaluator) (bool, error) {
	if gvk == listenerSetKind {
		return mutateListenerSetParentRef(ctx, obj, bodies, conditions)
	}

	if kind, supported := ingress.ClassTypeForGVK(gvk); supported {
		return mutateIngressClass(ctx, obj, kind, bodies, conditions)
	}

	return false, nil
}
