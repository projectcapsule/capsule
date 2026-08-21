// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package mutation

import (
	"fmt"
	"slices"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	resourcehelper "k8s.io/component-helpers/resource"

	apirules "github.com/projectcapsule/capsule/pkg/api/rules"
	"github.com/projectcapsule/capsule/pkg/runtime/workloads"
)

type workloadResourcePolicies struct {
	requests map[corev1.ResourceName]apirules.WorkloadResourceRequestPolicy
	limits   map[corev1.ResourceName]apirules.WorkloadResourceLimitPolicy
}

func MutateWorkloadResources(
	obj *unstructured.Unstructured,
	gvk schema.GroupVersionKind,
	bodies []*apirules.NamespaceRuleBodyNamespace,
) (bool, error) {
	if obj == nil || gvk != corev1.SchemeGroupVersion.WithKind("Pod") {
		return false, nil
	}

	pod := &corev1.Pod{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(obj.Object, pod); err != nil {
		return false, fmt.Errorf("decode Pod resource policies: %w", err)
	}

	changed, err := MutatePodResources(pod, bodies)
	if err != nil || !changed {
		return changed, err
	}

	mutated, err := runtime.DefaultUnstructuredConverter.ToUnstructured(pod)
	if err != nil {
		return false, fmt.Errorf("encode Pod resource policies: %w", err)
	}

	obj.Object = mutated

	return true, nil
}

func MutatePodResources(
	pod *corev1.Pod,
	bodies []*apirules.NamespaceRuleBodyNamespace,
) (bool, error) {
	if pod == nil {
		return false, nil
	}

	mutated := false

	containerPolicies := collectWorkloadResourcePolicies(bodies, apirules.ValidateContainers)
	for i := range pod.Spec.Containers {
		changed, err := mutateResourceRequirements(&pod.Spec.Containers[i].Resources, containerPolicies)
		if err != nil {
			return false, fmt.Errorf("spec.containers[%d].resources: %w", i, err)
		}

		mutated = mutated || changed
	}

	initContainerPolicies := collectWorkloadResourcePolicies(bodies, apirules.ValidateInitContainers)
	for i := range pod.Spec.InitContainers {
		changed, err := mutateResourceRequirements(&pod.Spec.InitContainers[i].Resources, initContainerPolicies)
		if err != nil {
			return false, fmt.Errorf("spec.initContainers[%d].resources: %w", i, err)
		}

		mutated = mutated || changed
	}

	podPolicies := collectWorkloadResourcePolicies(bodies, apirules.ValidatePod)
	if len(podPolicies.requests) > 0 || len(podPolicies.limits) > 0 {
		resources := pod.Spec.Resources
		if resources == nil {
			resources = &corev1.ResourceRequirements{}
		}

		changed, err := mutateResourceRequirements(resources, podPolicies)
		if err != nil {
			return false, fmt.Errorf("spec.resources: %w", err)
		}

		if changed {
			pod.Spec.Resources = resources
			mutated = true
		}
	}

	if mutated {
		backfillMissingPodRequests(pod)
	}

	return mutated, nil
}

// backfillMissingPodRequests keeps Pods mutated by Capsule valid on Kubernetes
// 1.33. That release validates every aggregate container request as soon as
// spec.resources is present, treating a missing Pod-level request as zero. We
// can backfill only resources Kubernetes permits at Pod level. Kubernetes 1.34
// and later skip missing keys; explicitly storing the effective aggregate keeps
// the same semantics on those releases.
func backfillMissingPodRequests(pod *corev1.Pod) {
	if pod.Spec.Resources == nil {
		return
	}

	requests := resourcehelper.AggregateContainerRequests(pod, resourcehelper.PodResourcesOptions{})

	for name, request := range requests {
		if !workloads.PodLevelResourceSupported(name) {
			continue
		}

		if _, found := pod.Spec.Resources.Requests[name]; found {
			continue
		}

		if pod.Spec.Resources.Requests == nil {
			pod.Spec.Resources.Requests = corev1.ResourceList{}
		}

		pod.Spec.Resources.Requests[name] = request.DeepCopy()
	}
}

func collectWorkloadResourcePolicies(
	bodies []*apirules.NamespaceRuleBodyNamespace,
	target apirules.WorkloadValidationTarget,
) workloadResourcePolicies {
	out := workloadResourcePolicies{}

	for _, body := range bodies {
		if body == nil || body.Enforce == nil || body.Enforce.Workloads.Resources == nil {
			continue
		}

		if !resourcePoliciesTarget(body.Enforce.Workloads, target) {
			continue
		}

		resources := body.Enforce.Workloads.Resources
		for name, policy := range resources.Requests {
			if !resourcePolicySupportsTarget(name, target) {
				continue
			}

			if out.requests == nil {
				out.requests = make(map[corev1.ResourceName]apirules.WorkloadResourceRequestPolicy)
			}

			out.requests[name] = policy
		}

		for name, policy := range resources.Limits {
			if !resourcePolicySupportsTarget(name, target) {
				continue
			}

			if out.limits == nil {
				out.limits = make(map[corev1.ResourceName]apirules.WorkloadResourceLimitPolicy)
			}

			out.limits[name] = policy
		}
	}

	return out
}

func resourcePoliciesTarget(
	workload apirules.NamespaceRuleEnforceWorkloadsBody,
	target apirules.WorkloadValidationTarget,
) bool {
	if len(workload.Targets) == 0 {
		return target == apirules.ValidatePod ||
			target == apirules.ValidateContainers ||
			target == apirules.ValidateInitContainers
	}

	return slices.Contains(workload.Targets, target)
}

func resourcePolicySupportsTarget(
	name corev1.ResourceName,
	target apirules.WorkloadValidationTarget,
) bool {
	return target != apirules.ValidatePod || workloads.PodLevelResourceSupported(name)
}

func mutateResourceRequirements(
	resources *corev1.ResourceRequirements,
	policies workloadResourcePolicies,
) (bool, error) {
	if resources == nil {
		return false, nil
	}

	requestsChanged, err := mutateRequestPolicies(resources, policies.requests)
	if err != nil {
		return false, err
	}

	limitsChanged, err := mutateLimitPolicies(resources, policies.limits)
	if err != nil {
		return false, err
	}

	return requestsChanged || limitsChanged, nil
}

func mutateRequestPolicies(
	resources *corev1.ResourceRequirements,
	policies map[corev1.ResourceName]apirules.WorkloadResourceRequestPolicy,
) (bool, error) {
	changed := false

	for name, policy := range policies {
		policyChanged, err := mutateRequestPolicy(resources, name, policy)
		if err != nil {
			return false, err
		}

		changed = policyChanged || changed
	}

	return changed, nil
}

func mutateRequestPolicy(
	resources *corev1.ResourceRequirements,
	name corev1.ResourceName,
	policy apirules.WorkloadResourceRequestPolicy,
) (bool, error) {
	switch policy.Policy {
	case apirules.WorkloadResourceRequestPolicyPreserve:
		return false, nil
	case apirules.WorkloadResourceRequestPolicyDefault:
		if policy.Value == nil {
			return false, fmt.Errorf("request %q Default policy has no value", name)
		}

		if _, found := resources.Requests[name]; found {
			return false, nil
		}

		if resources.Requests == nil {
			resources.Requests = corev1.ResourceList{}
		}

		resources.Requests[name] = policy.Value.DeepCopy()

		return true, nil
	case apirules.WorkloadResourceRequestPolicyRemove:
		return removeResource(resources.Requests, name), nil
	default:
		return false, fmt.Errorf("request %q has unsupported policy %q", name, policy.Policy)
	}
}

func mutateLimitPolicies(
	resources *corev1.ResourceRequirements,
	policies map[corev1.ResourceName]apirules.WorkloadResourceLimitPolicy,
) (bool, error) {
	changed := false

	for name, policy := range policies {
		policyChanged, err := mutateLimitPolicy(resources, name, policy)
		if err != nil {
			return false, err
		}

		changed = policyChanged || changed
	}

	return changed, nil
}

func mutateLimitPolicy(
	resources *corev1.ResourceRequirements,
	name corev1.ResourceName,
	policy apirules.WorkloadResourceLimitPolicy,
) (bool, error) {
	switch policy.Policy {
	case apirules.WorkloadResourceLimitPolicyPreserve:
		return false, nil
	case apirules.WorkloadResourceLimitPolicyDefault:
		if policy.Value == nil {
			return false, fmt.Errorf("limit %q Default policy has no value", name)
		}

		if _, found := resources.Limits[name]; found {
			return false, nil
		}

		if resources.Limits == nil {
			resources.Limits = corev1.ResourceList{}
		}

		resources.Limits[name] = policy.Value.DeepCopy()

		return true, nil
	case apirules.WorkloadResourceLimitPolicyRemove:
		return removeResource(resources.Limits, name), nil
	case apirules.WorkloadResourceLimitPolicyMatchRequest:
		return matchResourceRequest(resources, name), nil
	case apirules.WorkloadResourceLimitPolicyRatio:
		return defaultResourceRatio(resources, name, policy.Value)
	default:
		return false, fmt.Errorf("limit %q has unsupported policy %q", name, policy.Policy)
	}
}

func removeResource(resources corev1.ResourceList, name corev1.ResourceName) bool {
	if _, found := resources[name]; !found {
		return false
	}

	delete(resources, name)

	return true
}

func matchResourceRequest(resources *corev1.ResourceRequirements, name corev1.ResourceName) bool {
	request, found := resources.Requests[name]
	if !found {
		return false
	}

	if limit, found := resources.Limits[name]; found && limit.Cmp(request) == 0 {
		return false
	}

	if resources.Limits == nil {
		resources.Limits = corev1.ResourceList{}
	}

	resources.Limits[name] = request.DeepCopy()

	return true
}

func defaultResourceRatio(
	resources *corev1.ResourceRequirements,
	name corev1.ResourceName,
	ratio *resource.Quantity,
) (bool, error) {
	if ratio == nil {
		return false, fmt.Errorf("limit %q Ratio policy has no value", name)
	}

	if _, found := resources.Limits[name]; found {
		return false, nil
	}

	request, found := resources.Requests[name]
	if !found || request.Sign() <= 0 {
		return false, nil
	}

	limit, err := workloads.LimitForRatio(name, request, *ratio)
	if err != nil {
		return false, err
	}

	if resources.Limits == nil {
		resources.Limits = corev1.ResourceList{}
	}

	resources.Limits[name] = limit

	return true, nil
}
