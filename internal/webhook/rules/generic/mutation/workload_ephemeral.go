// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package mutation

import (
	"context"
	"fmt"
	"slices"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"

	"github.com/projectcapsule/capsule/pkg/api/rules"
	"github.com/projectcapsule/capsule/pkg/ruleengine"
)

func mutateEphemeralContainers(ctx context.Context, obj, old *unstructured.Unstructured, bodies []*rules.NamespaceRuleBodyNamespace, conditions *ruleengine.ConditionEvaluator) (bool, error) {
	applicable := func(mutation rules.NamespaceRuleMutation) bool {
		return hasContainerMutation(mutation.Workloads, true) && mutation.Workloads.GetWorkloadTargets(rules.ValidateEphemeralContainers)
	}

	if !slices.ContainsFunc(bodies, func(body *rules.NamespaceRuleBodyNamespace) bool {
		return body != nil && slices.ContainsFunc(body.Mutate, applicable)
	}) {
		return false, nil
	}

	if obj == nil || old == nil {
		return false, fmt.Errorf("ephemeral container mutation requires the new and old Pod")
	}

	pod := &corev1.Pod{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(obj.Object, pod); err != nil {
		return false, fmt.Errorf("decode Pod for ephemeral container mutation: %w", err)
	}

	linux := pod.Spec.OS == nil || pod.Spec.OS.Name != corev1.Windows

	existing, err := existingEphemeralContainerNames(old)
	if err != nil {
		return false, err
	}

	containers := workloadMutationContainers(pod, true, existing)
	if len(containers) == 0 {
		return false, nil
	}

	conditions.ResetObject()

	for i, body := range bodies {
		if body == nil {
			continue
		}

		for j, mutation := range body.Mutate {
			if !applicable(mutation) || !hasContainerMutation(mutation.Workloads, linux) {
				continue
			}

			matched, err := conditions.Matches(ctx, pod, mutation.Conditions)
			if err != nil {
				return false, fmt.Errorf("rules[%d].mutate[%d]: %w", i, j, err)
			}

			if !matched {
				continue
			}

			switch mutation.Action {
			case "", rules.MutationActionMerge, rules.MutationActionReplace:
				if mutateContainers(containers, mutation.Workloads, linux) {
					conditions.ResetObject()
				}
			default:
				return false, fmt.Errorf("rules[%d].mutate[%d].action: unsupported action %q", i, j, mutation.Action)
			}
		}
	}

	if !slices.ContainsFunc(containers, workloadMutationContainer.changed) {
		return false, nil
	}

	return writeEphemeralContainerMutations(obj, containers)
}

func writeEphemeralContainerMutations(obj *unstructured.Unstructured, containers []workloadMutationContainer) (bool, error) {
	// Write only changed scalar leaves, retaining unknown API fields and
	// every immutable existing container exactly as received.
	values, _, err := unstructured.NestedFieldNoCopy(obj.Object, "spec", "ephemeralContainers")
	if err != nil {
		return false, err
	}

	items, ok := values.([]any)
	if !ok {
		return false, fmt.Errorf("ephemeralContainers must be an array")
	}

	for _, container := range containers {
		if !container.changed() {
			continue
		}

		item, ok := items[container.index].(map[string]any)
		if !ok {
			return false, fmt.Errorf("ephemeralContainers[%d] must be an object", container.index)
		}

		if container.pullPolicyChanged() {
			item["imagePullPolicy"] = string(*container.pullPolicy)
		}

		if !container.rootFilesystemChanged() {
			continue
		}

		security, _ := item["securityContext"].(map[string]any)
		if security == nil {
			security = make(map[string]any)
			item["securityContext"] = security
		}

		security["readOnlyRootFilesystem"] = *rootFilesystemValue(*container.context)
	}

	return true, nil
}

func existingEphemeralContainerNames(old *unstructured.Unstructured) (map[string]struct{}, error) {
	oldValue, found, err := unstructured.NestedFieldNoCopy(old.Object, "spec", "ephemeralContainers")
	if err != nil {
		return nil, fmt.Errorf("read existing ephemeral containers: %w", err)
	}

	oldContainers, ok := oldValue.([]any)
	if found && oldValue != nil && !ok {
		return nil, fmt.Errorf("existing ephemeralContainers must be an array")
	}

	existing := make(map[string]struct{}, len(oldContainers))

	for _, value := range oldContainers {
		container, ok := value.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("existing ephemeral container must be an object")
		}

		name, ok := container["name"].(string)
		if !ok || name == "" {
			return nil, fmt.Errorf("existing ephemeral container must have a name")
		}

		existing[name] = struct{}{}
	}

	return existing, nil
}
