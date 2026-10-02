// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package mutation

import (
	"fmt"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"

	"github.com/projectcapsule/capsule/pkg/api/rules"
)

func BenchmarkPodResourceTargets(b *testing.B) {
	for _, count := range []int{1, 20} {
		b.Run(fmt.Sprintf("rules=%d", count), func(b *testing.B) {
			var bodies []*rules.NamespaceRuleBodyNamespace
			for range count {
				bodies = append(bodies, &rules.NamespaceRuleBodyNamespace{Enforce: &rules.NamespaceRuleEnforceBody{Workloads: rules.NamespaceRuleEnforceWorkloadsBody{Resources: &rules.WorkloadResourceRules{Requests: map[corev1.ResourceName]rules.WorkloadResourceRequestPolicy{corev1.ResourceMemory: {Policy: rules.WorkloadResourceRequestPolicyDefault, Value: new(resource.MustParse("32Mi"))}}, Limits: map[corev1.ResourceName]rules.WorkloadResourceLimitPolicy{corev1.ResourceMemory: {Policy: rules.WorkloadResourceLimitPolicyRatio, Value: new(resource.MustParse("2"))}}}}}})
			}
			pod := &corev1.Pod{Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "app", Image: "example.com/app:v1"}}}}
			b.ReportAllocs()
			for b.Loop() {
				pod.Spec.Resources = nil
				pod.Spec.Containers[0].Resources = corev1.ResourceRequirements{}
				changed, err := MutatePodResources(pod, bodies)
				if err != nil || !changed {
					b.Fatalf("mutation failed: changed=%v err=%v", changed, err)
				}
			}
		})
	}
}
