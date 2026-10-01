// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package mutation

import (
	"fmt"
	"slices"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/projectcapsule/capsule/pkg/api/rules"
	"github.com/projectcapsule/capsule/pkg/ruleengine"
	"github.com/projectcapsule/capsule/pkg/runtime/workloads"
)

func mutateTemplateResources(obj *unstructured.Unstructured, gvk schema.GroupVersionKind, bodies []*rules.NamespaceRuleBodyNamespace) (bool, error) {
	enforce := ruleengine.WorkloadEnforcement(ruleengine.EnforceBodiesFromNamespaceRules(bodies), gvk)
	if !slices.ContainsFunc(enforce, func(body *rules.NamespaceRuleEnforceBody) bool { return body != nil && body.Workloads.Resources != nil }) {
		return false, nil
	}

	obj.SetGroupVersionKind(gvk)

	pod, err := workloads.PodFromTemplate(obj)
	if err != nil {
		return false, err
	}

	changed, err := mutatePodResources(pod, enforce)
	if err != nil || !changed {
		return false, err
	}

	path := append(workloads.PodTemplatePath(gvk), "spec")
	// Write only changed resource locations so unknown template/Pod/container
	// fields survive a controller compiled against an older Kubernetes API.
	value, _, err := unstructured.NestedFieldNoCopy(obj.Object, path...)
	if err != nil {
		return false, err
	}

	spec, ok := value.(map[string]any)
	if !ok {
		return false, fmt.Errorf("pod template spec must be an object")
	}

	write := func(destination map[string]any, resources *corev1.ResourceRequirements) error {
		if resources == nil {
			return nil
		}

		encoded, err := runtime.DefaultUnstructuredConverter.ToUnstructured(resources)
		if err != nil {
			return err
		}

		existing, _ := destination["resources"].(map[string]any)

		for _, key := range []string{"requests", "limits"} {
			value, present := encoded[key]
			if present {
				if existing == nil {
					existing = map[string]any{}
					destination["resources"] = existing
				}

				existing[key] = value
			} else {
				delete(existing, key)
			}
		}

		return nil
	}
	if err := write(spec, pod.Spec.Resources); err != nil {
		return false, err
	}

	for _, group := range []struct {
		name       string
		containers []corev1.Container
	}{{"containers", pod.Spec.Containers}, {"initContainers", pod.Spec.InitContainers}} {
		original, _ := spec[group.name].([]any)
		for i := range group.containers {
			if i >= len(original) {
				return false, fmt.Errorf("template %s[%d] is missing", group.name, i)
			}

			container, ok := original[i].(map[string]any)
			if !ok {
				return false, fmt.Errorf("template %s[%d] must be an object", group.name, i)
			}

			if err := write(container, &group.containers[i].Resources); err != nil {
				return false, err
			}
		}
	}

	return true, nil
}
