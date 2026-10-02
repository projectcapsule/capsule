// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package mutation

import (
	corev1 "k8s.io/api/core/v1"
	"k8s.io/utils/ptr"

	"github.com/projectcapsule/capsule/pkg/api/rules"
)

// Keep only original scalar values, not copies of complete containers.
// Snapshots also detect ordered rules undoing prior writes.
type workloadMutationContainer struct {
	target             rules.WorkloadValidationTarget
	index              int
	context            **corev1.SecurityContext
	original           *bool
	pullPolicy         *corev1.PullPolicy
	originalPullPolicy corev1.PullPolicy
}

func rootFilesystemValue(sc *corev1.SecurityContext) *bool {
	if sc == nil {
		return nil
	}

	return sc.ReadOnlyRootFilesystem
}

func workloadMutationContainers(pod *corev1.Pod, ephemeralOnly bool, existing map[string]struct{}) []workloadMutationContainer {
	capacity := len(pod.Spec.EphemeralContainers)
	if !ephemeralOnly {
		capacity += len(pod.Spec.Containers) + len(pod.Spec.InitContainers)
	}

	containers := make([]workloadMutationContainer, 0, capacity)

	if !ephemeralOnly {
		for _, group := range []struct {
			target rules.WorkloadValidationTarget
			items  []corev1.Container
		}{{rules.ValidateContainers, pod.Spec.Containers}, {rules.ValidateInitContainers, pod.Spec.InitContainers}} {
			for i := range group.items {
				sc := &group.items[i].SecurityContext
				containers = append(containers, workloadMutationContainer{target: group.target, index: i, context: sc, original: rootFilesystemValue(*sc), pullPolicy: &group.items[i].ImagePullPolicy, originalPullPolicy: group.items[i].ImagePullPolicy})
			}
		}
	}

	for i := range pod.Spec.EphemeralContainers {
		container := &pod.Spec.EphemeralContainers[i]
		if _, found := existing[container.Name]; found {
			continue
		}

		sc := &container.SecurityContext
		containers = append(containers, workloadMutationContainer{target: rules.ValidateEphemeralContainers, index: i, context: sc, original: rootFilesystemValue(*sc), pullPolicy: &container.ImagePullPolicy, originalPullPolicy: container.ImagePullPolicy})
	}

	return containers
}

// Container properties overwrite selected values under both merge and replace.
func mutateContainers(containers []workloadMutationContainer, workload rules.WorkloadMutation, linux bool) bool {
	changed := false

	for _, container := range containers {
		if !workload.GetWorkloadTargets(container.target) {
			continue
		}

		if workload.Registries.ImagePullPolicy != "" && *container.pullPolicy != workload.Registries.ImagePullPolicy {
			*container.pullPolicy = workload.Registries.ImagePullPolicy
			changed = true
		}

		if !linux || workload.ReadOnlyRootFilesystem == nil || ptr.Equal(rootFilesystemValue(*container.context), workload.ReadOnlyRootFilesystem) {
			continue
		}

		if *container.context == nil {
			*container.context = &corev1.SecurityContext{}
		}

		(*container.context).ReadOnlyRootFilesystem = new(*workload.ReadOnlyRootFilesystem)
		changed = true
	}

	return changed
}

func (c workloadMutationContainer) changed() bool {
	return c.pullPolicyChanged() || c.rootFilesystemChanged()
}

func (c workloadMutationContainer) pullPolicyChanged() bool {
	return c.originalPullPolicy != *c.pullPolicy
}

func (c workloadMutationContainer) rootFilesystemChanged() bool {
	return !ptr.Equal(c.original, rootFilesystemValue(*c.context))
}

func hasContainerMutation(workload rules.WorkloadMutation, linux bool) bool {
	return workload.Registries.ImagePullPolicy != "" || (linux && workload.ReadOnlyRootFilesystem != nil)
}
