// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package workloads

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/projectcapsule/capsule/pkg/api/rules"
)

// PodTemplatePath recognizes only the supported native controller groups.
func PodTemplatePath(gvk schema.GroupVersionKind) []string {
	kind, ok := rules.WorkloadTargetForGVK(gvk)
	if !ok || kind == rules.ValidatePod {
		return nil
	}

	if kind == rules.ValidateCronJob {
		return []string{"spec", "jobTemplate", "spec", "template"}
	}

	return []string{"spec", "template"}
}

// PodFromTemplate decodes the Pod-spec projection used by existing validators.
// The controller object remains the admission/condition/event identity.
func PodFromTemplate(obj *unstructured.Unstructured) (*corev1.Pod, error) {
	path := PodTemplatePath(obj.GroupVersionKind())
	if len(path) == 0 {
		return nil, fmt.Errorf("unsupported workload %s", obj.GroupVersionKind())
	}

	value, found, err := unstructured.NestedFieldNoCopy(obj.Object, path...)
	if err != nil {
		return nil, err
	}

	if !found {
		return nil, fmt.Errorf("pod template is missing")
	}

	template, ok := value.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("pod template must be an object")
	}

	pod := &corev1.Pod{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(template, pod); err != nil {
		return nil, fmt.Errorf("decode Pod template: %w", err)
	}

	pod.Namespace = obj.GetNamespace()

	return pod, nil
}
