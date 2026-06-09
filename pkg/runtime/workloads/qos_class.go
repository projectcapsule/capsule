// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package workloads

import (
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

func GetPodQoSClass(pod *corev1.Pod) corev1.PodQOSClass {
	if pod == nil {
		return corev1.PodQOSBestEffort
	}

	// Pod Can not change QOSClass during it's lifetime. Therefore we can use the status value if present.
	// Docs: The QoS class is determined when the Pod is created and remains unchanged for the lifetime of the Pod. If you later attempt an in-place resize that would result in a different QoS class, the resize is rejected by admission.
	if pod.Status.QOSClass != "" {
		return pod.Status.QOSClass
	}

	return computePodQoSClass(pod)
}

func computePodQoSClass(pod *corev1.Pod) corev1.PodQOSClass {
	if podLevelQoS, ok := computePodLevelQoSClass(pod); ok {
		return podLevelQoS
	}

	return computeContainerLevelQoSClass(pod)
}

func computePodLevelQoSClass(pod *corev1.Pod) (corev1.PodQOSClass, bool) {
	if pod == nil {
		return corev1.PodQOSBestEffort, false
	}

	if pod.Spec.Resources == nil {
		return corev1.PodQOSBestEffort, false
	}

	requests := pod.Spec.Resources.Requests
	limits := pod.Spec.Resources.Limits

	if !hasSupportedQoSResource(requests) && !hasSupportedQoSResource(limits) {
		return corev1.PodQOSBestEffort, false
	}

	cpuRequest, hasCPURequest := positiveResource(requests, corev1.ResourceCPU)
	memoryRequest, hasMemoryRequest := positiveResource(requests, corev1.ResourceMemory)
	cpuLimit, hasCPULimit := positiveResource(limits, corev1.ResourceCPU)
	memoryLimit, hasMemoryLimit := positiveResource(limits, corev1.ResourceMemory)

	if hasCPURequest &&
		hasMemoryRequest &&
		hasCPULimit &&
		hasMemoryLimit &&
		cpuRequest.Cmp(cpuLimit) == 0 &&
		memoryRequest.Cmp(memoryLimit) == 0 {
		return corev1.PodQOSGuaranteed, true
	}

	return corev1.PodQOSBurstable, true
}

func computeContainerLevelQoSClass(pod *corev1.Pod) corev1.PodQOSClass {
	requests := corev1.ResourceList{}
	limits := corev1.ResourceList{}
	isGuaranteed := true

	containers := make([]corev1.Container, 0,
		len(pod.Spec.Containers)+
			len(pod.Spec.InitContainers)+
			len(pod.Spec.EphemeralContainers),
	)

	containers = append(containers, pod.Spec.Containers...)
	containers = append(containers, pod.Spec.InitContainers...)

	for _, container := range pod.Spec.EphemeralContainers {
		containers = append(containers, corev1.Container{
			Name:      container.Name,
			Resources: container.Resources,
		})
	}

	for _, container := range containers {
		containerLimitsFound := map[corev1.ResourceName]struct{}{}

		for name, quantity := range container.Resources.Requests {
			if !isSupportedQoSComputeResource(name) || quantity.Sign() <= 0 {
				continue
			}

			addResource(requests, name, quantity)
		}

		for name, quantity := range container.Resources.Limits {
			if !isSupportedQoSComputeResource(name) || quantity.Sign() <= 0 {
				continue
			}

			containerLimitsFound[name] = struct{}{}

			addResource(limits, name, quantity)
		}

		if _, ok := containerLimitsFound[corev1.ResourceCPU]; !ok {
			isGuaranteed = false
		}

		if _, ok := containerLimitsFound[corev1.ResourceMemory]; !ok {
			isGuaranteed = false
		}
	}

	if len(requests) == 0 && len(limits) == 0 {
		return corev1.PodQOSBestEffort
	}

	if isGuaranteed {
		for name, request := range requests {
			limit, ok := limits[name]
			if !ok || request.Cmp(limit) != 0 {
				isGuaranteed = false

				break
			}
		}
	}

	if isGuaranteed && len(requests) == len(limits) {
		return corev1.PodQOSGuaranteed
	}

	return corev1.PodQOSBurstable
}

func hasSupportedQoSResource(resources corev1.ResourceList) bool {
	for name, quantity := range resources {
		if isSupportedQoSComputeResource(name) && quantity.Sign() > 0 {
			return true
		}
	}

	return false
}

func positiveResource(resources corev1.ResourceList, name corev1.ResourceName) (resource.Quantity, bool) {
	quantity, ok := resources[name]
	if !ok || quantity.Sign() <= 0 {
		return resource.Quantity{}, false
	}

	return quantity, true
}

func addResource(resources corev1.ResourceList, name corev1.ResourceName, quantity resource.Quantity) {
	current, ok := resources[name]
	if !ok {
		resources[name] = quantity.DeepCopy()

		return
	}

	current.Add(quantity)
	resources[name] = current
}

//nolint:exhaustive
func isSupportedQoSComputeResource(name corev1.ResourceName) bool {
	switch name {
	case corev1.ResourceCPU, corev1.ResourceMemory:
		return true
	default:
		return false
	}
}
