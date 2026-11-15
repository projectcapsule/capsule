// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package replication

import (
	"context"
	"errors"
	"slices"

	admissionv1 "k8s.io/api/admissionregistration/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	celruntime "github.com/projectcapsule/capsule/pkg/runtime/cel"
)

type SelectorCompiler interface {
	GetOrCompile(*metav1.LabelSelector) (labels.Selector, error)
}
type WatchConditionCompiler interface {
	GetOrCompileTriggerCondition(string) (*celruntime.CompiledExpression, error)
}

type preparedTrigger struct {
	keys              map[string]struct{}
	namespace, object labels.Selector
	conditions        []*celruntime.CompiledExpression
}

// PreparedTriggers is immutable. It retains compiled artifacts, never object
// snapshots, tenant decisions, evaluation inputs or results.
type PreparedTriggers struct {
	rules          []preparedTrigger
	resources      map[schema.GroupVersionResource]struct{}
	needsNamespace bool
}

func PrepareTriggers(spec *capsulev1beta2.TenantResourceCommonSpec, namespaced bool, selectors SelectorCompiler, conditions WatchConditionCompiler) (*PreparedTriggers, error) {
	if err := ValidateTriggerStructure(spec, namespaced); err != nil {
		return nil, err
	}

	prepared := &PreparedTriggers{resources: make(map[schema.GroupVersionResource]struct{})}

	for _, trigger := range spec.Triggers {
		rule := preparedTrigger{keys: make(map[string]struct{}), namespace: labels.Everything(), object: labels.Everything()}

		var err error
		if trigger.NamespaceSelector != nil {
			rule.namespace, err = selectors.GetOrCompile(trigger.NamespaceSelector)
			if err != nil {
				return nil, err
			}

			prepared.needsNamespace = prepared.needsNamespace || !rule.namespace.Empty()
		}

		if trigger.ObjectSelector != nil {
			rule.object, err = selectors.GetOrCompile(trigger.ObjectSelector)
			if err != nil {
				return nil, err
			}
		}

		for _, resource := range TriggerResources(&trigger) {
			prepared.resources[resource] = struct{}{}

			for _, operation := range []admissionv1.OperationType{admissionv1.Create, admissionv1.Update, admissionv1.Delete} {
				if slices.Contains(trigger.Operations, operation) || slices.Contains(trigger.Operations, admissionv1.OperationAll) {
					rule.keys[ResourceKey(resource, operation)] = struct{}{}
				}
			}
		}

		for _, condition := range trigger.MatchConditions {
			compiled, err := conditions.GetOrCompileTriggerCondition(condition.Expression)
			if err != nil {
				return nil, err
			}

			rule.conditions = append(rule.conditions, compiled)
		}

		prepared.rules = append(prepared.rules, rule)
	}

	return prepared, nil
}

func (p *PreparedTriggers) Resources() []schema.GroupVersionResource {
	out := make([]schema.GroupVersionResource, 0, len(p.resources))
	for resource := range p.resources {
		out = append(out, resource)
	}

	return out
}

func (p *PreparedTriggers) NeedsNamespace() bool { return p.needsNamespace }

func (p *PreparedTriggers) Matches(ctx context.Context, key string, operation admissionv1.OperationType, namespaceLabels map[string]string, oldObject, object *unstructured.Unstructured) (bool, error) {
	return p.MatchesWithNamespace(ctx, key, operation, namespaceLabels, true, oldObject, object)
}

// MatchesWithNamespace skips only alternatives requiring namespace labels when
// namespace metadata is unavailable. Unrestricted alternatives remain eligible.
func (p *PreparedTriggers) MatchesWithNamespace(ctx context.Context, key string, operation admissionv1.OperationType, namespaceLabels map[string]string, namespaceAvailable bool, oldObject, object *unstructured.Unstructured) (bool, error) {
	var (
		evaluationError error
		activation      map[string]any
		oldLabels       labels.Set
		newLabels       labels.Set
		labelsRead      bool
	)

	for _, rule := range p.rules {
		if _, ok := rule.keys[key]; !ok {
			continue
		}

		if !namespaceAvailable && !rule.namespace.Empty() {
			continue
		}

		if !rule.namespace.Matches(labels.Set(namespaceLabels)) {
			continue
		}

		if !labelsRead {
			if oldObject != nil {
				oldLabels = labels.Set(oldObject.GetLabels())
			}

			if object != nil {
				newLabels = labels.Set(object.GetLabels())
			}

			labelsRead = true
		}

		oldMatch := oldObject != nil && rule.object.Matches(oldLabels)

		newMatch := object != nil && rule.object.Matches(newLabels)
		if !oldMatch && !newMatch {
			continue
		}

		if len(rule.conditions) == 0 {
			return true, nil
		}

		if activation == nil {
			activation = map[string]any{"object": nil, "oldObject": nil, "operation": string(operation)}
			if object != nil {
				activation["object"] = object.Object
			}

			if oldObject != nil {
				activation["oldObject"] = oldObject.Object
			}
		}

		matches := true

		for _, condition := range rule.conditions {
			matched, err := condition.EvaluateBooleanWithVariables(ctx, activation)
			if err != nil {
				evaluationError = errors.Join(evaluationError, err)
				matches = false

				break
			}

			if !matched {
				matches = false

				break
			}
		}

		if matches {
			return true, nil
		}
	}

	return false, evaluationError
}
