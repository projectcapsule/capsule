// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package ruleengine

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"

	"github.com/projectcapsule/capsule/pkg/api/rules"
)

func TestImagePullSecretValidationLimits(t *testing.T) {
	for _, count := range []int{64, 65} {
		workload := rules.WorkloadMutation{}
		for i := 0; i < count; i++ {
			workload.Registries.ImagePullSecrets = append(workload.Registries.ImagePullSecrets, corev1.LocalObjectReference{Name: fmt.Sprintf("pull-%d", i)})
		}
		err := validateMutationPlacement("workloads", workload)
		if count == 64 {
			require.NoError(t, err)
		} else {
			require.ErrorContains(t, err, "at most 64")
		}
	}
	for _, name := range []string{"", " ", "wrong/name", "Upper", strings.Repeat("a", 254)} {
		err := validateMutationPlacement("workloads", rules.WorkloadMutation{Registries: rules.WorkloadRegistryMutation{ImagePullSecrets: []corev1.LocalObjectReference{{Name: name}}}})
		require.ErrorContains(t, err, "registries.imagePullSecrets[0].name")
	}
}

func BenchmarkValidateImagePullSecrets(b *testing.B) {
	for _, ruleCount := range []int{1, 20} {
		for _, count := range []int{0, 1, 64, 65} {
			b.Run(fmt.Sprintf("rules=%d/secrets=%d", ruleCount, count), func(b *testing.B) {
				bodies := make([]*rules.NamespaceRuleBodyNamespace, ruleCount)
				for i := range bodies {
					registry := rules.WorkloadRegistryMutation{ImagePullPolicy: corev1.PullAlways}
					for j := 0; j < count; j++ {
						registry.ImagePullSecrets = append(registry.ImagePullSecrets, corev1.LocalObjectReference{Name: fmt.Sprintf("registry-%d-%d", i, j)})
					}
					bodies[i] = &rules.NamespaceRuleBodyNamespace{Mutate: []rules.NamespaceRuleMutation{{Workloads: rules.WorkloadMutation{Registries: registry}}}}
				}
				b.ReportAllocs()
				for b.Loop() {
					err := ValidateRuleStatusBody(nil, bodies)
					if count <= 64 && err != nil {
						b.Fatal(err)
					}
					if count > 64 && err == nil {
						b.Fatal("oversized list accepted")
					}
				}
			})
		}
	}
}
