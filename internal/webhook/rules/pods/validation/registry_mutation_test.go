// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package validation

import (
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"

	"github.com/projectcapsule/capsule/internal/cache"
	"github.com/projectcapsule/capsule/internal/webhook/rules/generic/mutation"
	"github.com/projectcapsule/capsule/pkg/api/rules"
	"github.com/projectcapsule/capsule/pkg/api/runtime"
)

func TestRegistryEnforcementAfterNestedWorkloadMutation(t *testing.T) {
	const image = "registry.k8s.io/pause:3.10"
	for _, action := range []rules.MutationAction{rules.MutationActionMerge, rules.MutationActionReplace} {
		for _, tc := range []struct {
			name, exact, denied string
			initial, desired    corev1.PullPolicy
		}{
			{"matching image with forbidden policy", image, `image pull policy "Never" at initContainers[0] is not allowed`, corev1.PullAlways, corev1.PullNever},
			{"matching image with allowed policy", image, "", corev1.PullNever, corev1.PullAlways},
			{"hostname alone does not match the image", "registry.k8s.io", `registry "registry.k8s.io/pause:3.10" at initContainers[0] is not allowed`, corev1.PullNever, corev1.PullAlways},
		} {
			t.Run(string(action)+"/"+tc.name, func(t *testing.T) {
				pod := registryPodForTest(image, tc.initial)
				pod.Spec.InitContainers = []corev1.Container{{Name: "init", Image: image, ImagePullPolicy: tc.initial}}
				body := &rules.NamespaceRuleBodyNamespace{
					Mutate: []rules.NamespaceRuleMutation{{Action: action, Workloads: rules.WorkloadMutation{
						Placement: rules.WorkloadPlacementMutation{Scheduler: "tenant-scheduler"},
						Security:  rules.WorkloadSecurityMutation{ReadOnlyRootFilesystem: new(true)},
						Registries: rules.WorkloadRegistryMutation{
							ImagePullPolicy:  tc.desired,
							ImagePullSecrets: []corev1.LocalObjectReference{{Name: "tenant-registry"}},
						},
					}}},
					Enforce: registryEnforceForTest(rules.ActionTypeAllow, []corev1.PullPolicy{corev1.PullAlways}, rules.OCIRegistry{ExpressionMatch: runtime.ExpressionMatch{Exact: []string{tc.exact}}}),
				}
				original := body.DeepCopy()
				changed, err := mutation.MutatePodPlacement(t.Context(), pod, []*rules.NamespaceRuleBodyNamespace{body}, nil)
				require.NoError(t, err)
				require.True(t, changed)
				require.Equal(t, "tenant-scheduler", pod.Spec.SchedulerName)
				require.Equal(t, []corev1.LocalObjectReference{{Name: "tenant-registry"}}, pod.Spec.ImagePullSecrets)
				for _, container := range append(pod.Spec.InitContainers, pod.Spec.Containers...) {
					require.Equal(t, tc.desired, container.ImagePullPolicy)
					require.NotNil(t, container.SecurityContext)
					require.Equal(t, new(true), container.SecurityContext.ReadOnlyRootFilesystem)
				}
				h := &podRules{registryCache: cache.NewRegistryRuleSetCache(cache.NewRegexCache())}
				evaluation, err := h.validateRegistries(pod, []*rules.NamespaceRuleEnforceBody{body.Enforce})
				require.NoError(t, err)
				require.NotNil(t, evaluation)
				if tc.denied == "" {
					require.NoError(t, evaluation.BlockingError())
				} else {
					require.ErrorContains(t, evaluation.BlockingError(), tc.denied)
				}
				require.Equal(t, original, body, "mutation and enforcement must preserve rule-owned values")
			})
		}
	}
}
