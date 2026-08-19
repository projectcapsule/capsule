// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package mutation

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"

	"github.com/projectcapsule/capsule/pkg/api/rules"
)

func TestMutatePodResourcesUsesContainerTargets(t *testing.T) {
	t.Parallel()

	pod := &corev1.Pod{Spec: corev1.PodSpec{
		Containers: []corev1.Container{{
			Name: "app",
			Resources: corev1.ResourceRequirements{
				Requests: corev1.ResourceList{corev1.ResourceMemory: resource.MustParse("1Gi")},
				Limits:   corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("1")},
			},
		}},
		InitContainers: []corev1.Container{{
			Name: "init",
			Resources: corev1.ResourceRequirements{
				Requests: corev1.ResourceList{corev1.ResourceMemory: resource.MustParse("512Mi")},
			},
		}},
		Resources: &corev1.ResourceRequirements{
			Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("500m")},
		},
	}}

	bodies := []*rules.NamespaceRuleBodyNamespace{{Enforce: &rules.NamespaceRuleEnforceBody{
		Workloads: rules.NamespaceRuleEnforceWorkloadsBody{
			Resources: &rules.WorkloadResourceRules{
				Requests: map[corev1.ResourceName]rules.WorkloadResourceRequestPolicy{
					corev1.ResourceCPU: {
						Policy: rules.WorkloadResourceRequestPolicyDefault,
						Value:  quantityPointer("100m"),
					},
				},
				Limits: map[corev1.ResourceName]rules.WorkloadResourceLimitPolicy{
					corev1.ResourceCPU: {Policy: rules.WorkloadResourceLimitPolicyRemove},
					corev1.ResourceMemory: {
						Policy: rules.WorkloadResourceLimitPolicyMatchRequest,
					},
				},
			},
		},
	}}}

	changed, err := MutatePodResources(pod, bodies)
	if err != nil {
		t.Fatalf("MutatePodResources() error = %v", err)
	}
	if !changed {
		t.Fatal("MutatePodResources() changed = false, want true")
	}

	assertResourceQuantity(t, pod.Spec.Containers[0].Resources.Requests, corev1.ResourceCPU, "100m")
	assertResourceMissing(t, pod.Spec.Containers[0].Resources.Limits, corev1.ResourceCPU)
	assertResourceQuantity(t, pod.Spec.Containers[0].Resources.Limits, corev1.ResourceMemory, "1Gi")
	assertResourceQuantity(t, pod.Spec.InitContainers[0].Resources.Requests, corev1.ResourceCPU, "100m")
	assertResourceQuantity(t, pod.Spec.InitContainers[0].Resources.Limits, corev1.ResourceMemory, "512Mi")

	// Pod-level resources are opt-in even when targets is empty.
	assertResourceQuantity(t, pod.Spec.Resources.Requests, corev1.ResourceCPU, "500m")
	assertResourceMissing(t, pod.Spec.Resources.Limits, corev1.ResourceCPU)
}

func TestMutatePodResourcesPodRatio(t *testing.T) {
	t.Parallel()

	pod := &corev1.Pod{Spec: corev1.PodSpec{
		Resources: &corev1.ResourceRequirements{
			Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("500m")},
		},
	}}

	bodies := []*rules.NamespaceRuleBodyNamespace{{Enforce: &rules.NamespaceRuleEnforceBody{
		Workloads: rules.NamespaceRuleEnforceWorkloadsBody{
			Targets: []rules.WorkloadValidationTarget{rules.ValidatePod},
			Resources: &rules.WorkloadResourceRules{
				Limits: map[corev1.ResourceName]rules.WorkloadResourceLimitPolicy{
					corev1.ResourceCPU: {
						Policy: rules.WorkloadResourceLimitPolicyRatio,
						Value:  quantityPointer("1.5"),
					},
				},
			},
		},
	}}}

	changed, err := MutatePodResources(pod, bodies)
	if err != nil {
		t.Fatalf("MutatePodResources() error = %v", err)
	}
	if !changed {
		t.Fatal("MutatePodResources() changed = false, want true")
	}

	assertResourceQuantity(t, pod.Spec.Resources.Limits, corev1.ResourceCPU, "750m")
}

func TestMutatePodResourcesPreservesExplicitRatioLimit(t *testing.T) {
	t.Parallel()

	pod := &corev1.Pod{Spec: corev1.PodSpec{Containers: []corev1.Container{{
		Name: "app",
		Resources: corev1.ResourceRequirements{
			Requests: corev1.ResourceList{corev1.ResourceMemory: resource.MustParse("1Gi")},
			Limits:   corev1.ResourceList{corev1.ResourceMemory: resource.MustParse("2Gi")},
		},
	}}}}

	bodies := resourceMutationBodies(
		rules.WorkloadResourceLimitPolicyRatio,
		quantityPointer("1.5"),
	)

	changed, err := MutatePodResources(pod, bodies)
	if err != nil {
		t.Fatalf("MutatePodResources() error = %v", err)
	}
	if changed {
		t.Fatal("MutatePodResources() changed an explicitly supplied ratio limit")
	}

	assertResourceQuantity(t, pod.Spec.Containers[0].Resources.Limits, corev1.ResourceMemory, "2Gi")
}

func TestMutatePodResourcesLastPolicyWins(t *testing.T) {
	t.Parallel()

	pod := &corev1.Pod{Spec: corev1.PodSpec{Containers: []corev1.Container{{
		Name: "app",
		Resources: corev1.ResourceRequirements{
			Limits: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("1")},
		},
	}}}}

	bodies := []*rules.NamespaceRuleBodyNamespace{
		{Enforce: &rules.NamespaceRuleEnforceBody{Workloads: rules.NamespaceRuleEnforceWorkloadsBody{
			Resources: &rules.WorkloadResourceRules{Limits: map[corev1.ResourceName]rules.WorkloadResourceLimitPolicy{
				corev1.ResourceCPU: {Policy: rules.WorkloadResourceLimitPolicyRemove},
			}},
		}}},
		{Enforce: &rules.NamespaceRuleEnforceBody{Workloads: rules.NamespaceRuleEnforceWorkloadsBody{
			Targets: []rules.WorkloadValidationTarget{rules.ValidateContainers},
			Resources: &rules.WorkloadResourceRules{Limits: map[corev1.ResourceName]rules.WorkloadResourceLimitPolicy{
				corev1.ResourceCPU: {Policy: rules.WorkloadResourceLimitPolicyPreserve},
			}},
		}}},
	}

	changed, err := MutatePodResources(pod, bodies)
	if err != nil {
		t.Fatalf("MutatePodResources() error = %v", err)
	}
	if changed {
		t.Fatal("MutatePodResources() ignored the later Preserve policy")
	}

	assertResourceQuantity(t, pod.Spec.Containers[0].Resources.Limits, corev1.ResourceCPU, "1")
}

func resourceMutationBodies(
	policy rules.WorkloadResourceLimitPolicyType,
	value *resource.Quantity,
) []*rules.NamespaceRuleBodyNamespace {
	return []*rules.NamespaceRuleBodyNamespace{{Enforce: &rules.NamespaceRuleEnforceBody{
		Workloads: rules.NamespaceRuleEnforceWorkloadsBody{
			Resources: &rules.WorkloadResourceRules{
				Limits: map[corev1.ResourceName]rules.WorkloadResourceLimitPolicy{
					corev1.ResourceMemory: {Policy: policy, Value: value},
				},
			},
		},
	}}}
}

func quantityPointer(value string) *resource.Quantity {
	quantity := resource.MustParse(value)

	return &quantity
}

func assertResourceQuantity(
	t *testing.T,
	resources corev1.ResourceList,
	name corev1.ResourceName,
	want string,
) {
	t.Helper()

	got, found := resources[name]
	if !found {
		t.Fatalf("resource %q is missing", name)
	}

	wantQuantity := resource.MustParse(want)
	if got.Cmp(wantQuantity) != 0 {
		t.Fatalf("resource %q = %s, want %s", name, got.String(), wantQuantity.String())
	}
}

func assertResourceMissing(t *testing.T, resources corev1.ResourceList, name corev1.ResourceName) {
	t.Helper()

	if value, found := resources[name]; found {
		t.Fatalf("resource %q = %s, want missing", name, value.String())
	}
}
