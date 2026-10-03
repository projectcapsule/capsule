// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package mutation

import (
	corev1 "k8s.io/api/core/v1"
	"k8s.io/utils/ptr"

	"github.com/projectcapsule/capsule/pkg/api/rules"
)

// Keep only the original Boolean pointers, not copies of complete containers.
// Writes replace pointers so this snapshot also detects rules undoing prior writes.
type rootFilesystemContainer struct {
	target   rules.WorkloadValidationTarget
	index    int
	context  **corev1.SecurityContext
	original *bool
}

func rootFilesystemValue(sc *corev1.SecurityContext) *bool {
	if sc == nil {
		return nil
	}

	return sc.ReadOnlyRootFilesystem
}

func rootFilesystemContainers(pod *corev1.Pod, ephemeralOnly bool, existing map[string]struct{}) []rootFilesystemContainer {
	capacity := len(pod.Spec.EphemeralContainers)
	if !ephemeralOnly {
		capacity += len(pod.Spec.Containers) + len(pod.Spec.InitContainers)
	}

	containers := make([]rootFilesystemContainer, 0, capacity)

	if !ephemeralOnly {
		for _, group := range []struct {
			target rules.WorkloadValidationTarget
			items  []corev1.Container
		}{{rules.ValidateContainers, pod.Spec.Containers}, {rules.ValidateInitContainers, pod.Spec.InitContainers}} {
			for i := range group.items {
				sc := &group.items[i].SecurityContext
				containers = append(containers, rootFilesystemContainer{target: group.target, index: i, context: sc, original: rootFilesystemValue(*sc)})
			}
		}
	}

	for i := range pod.Spec.EphemeralContainers {
		container := &pod.Spec.EphemeralContainers[i]
		if _, found := existing[container.Name]; found {
			continue
		}

		sc := &container.SecurityContext
		containers = append(containers, rootFilesystemContainer{target: rules.ValidateEphemeralContainers, index: i, context: sc, original: rootFilesystemValue(*sc)})
	}

	return containers
}

func mutateRootFilesystems(containers []rootFilesystemContainer, workload rules.WorkloadMutation) bool {
	changed := false

	for _, container := range containers {
		if !workload.GetWorkloadTargets(container.target) || ptr.Equal(rootFilesystemValue(*container.context), workload.Security.ReadOnlyRootFilesystem) {
			continue
		}

		if *container.context == nil {
			*container.context = &corev1.SecurityContext{}
		}

		(*container.context).ReadOnlyRootFilesystem = new(*workload.Security.ReadOnlyRootFilesystem)
		changed = true
	}

	return changed
}

func (c rootFilesystemContainer) changed() bool {
	return !ptr.Equal(c.original, rootFilesystemValue(*c.context))
}
