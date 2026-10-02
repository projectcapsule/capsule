// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package mutation

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/utils/ptr"

	"github.com/projectcapsule/capsule/pkg/api/rules"
)

func rootFilesystemPod(initial *bool) *corev1.Pod {
	security := &corev1.SecurityContext{ReadOnlyRootFilesystem: initial, RunAsUser: new(int64(1000))}
	return &corev1.Pod{Spec: corev1.PodSpec{
		Containers:          []corev1.Container{{Name: "app", Image: "example.com/app", SecurityContext: security.DeepCopy()}},
		InitContainers:      []corev1.Container{{Name: "init", Image: "example.com/init", SecurityContext: security.DeepCopy()}, {Name: "sidecar", Image: "example.com/sidecar", RestartPolicy: new(corev1.ContainerRestartPolicyAlways), SecurityContext: security.DeepCopy()}},
		EphemeralContainers: []corev1.EphemeralContainer{{EphemeralContainerCommon: corev1.EphemeralContainerCommon{Name: "debug", Image: "example.com/debug", SecurityContext: security.DeepCopy()}}},
	}}
}

func rootFilesystemBody(value *bool, targets ...rules.WorkloadValidationTarget) *rules.NamespaceRuleBodyNamespace {
	return &rules.NamespaceRuleBodyNamespace{Mutate: []rules.NamespaceRuleMutation{{Workloads: rules.WorkloadMutation{ReadOnlyRootFilesystem: value, Targets: targets}}}}
}

func TestReadOnlyRootFilesystemActionsAndPresence(t *testing.T) {
	for _, action := range []rules.MutationAction{"", rules.MutationActionMerge, rules.MutationActionReplace} {
		for i, input := range []*bool{nil, new(false), new(true)} {
			for j, desired := range []*bool{nil, new(false), new(true)} {
				t.Run(fmt.Sprintf("%s/input=%d/desired=%d", action, i, j), func(t *testing.T) {
					pod := rootFilesystemPod(input)
					body := rootFilesystemBody(desired)
					body.Mutate[0].Action = action
					original := body.DeepCopy()
					want := input
					if desired != nil {
						want = desired
					}
					for pass := range 2 {
						changed, err := MutatePodPlacement(t.Context(), pod, []*rules.NamespaceRuleBodyNamespace{body}, nil)
						require.NoError(t, err)
						require.Equal(t, pass == 0 && !ptr.Equal(input, want), changed)
						for _, container := range rootFilesystemContainers(pod, false, nil) {
							require.Equal(t, want, rootFilesystemValue(*container.context))
							require.Equal(t, int64(1000), *(*container.context).RunAsUser)
						}
					}
					if desired != nil {
						*pod.Spec.Containers[0].SecurityContext.ReadOnlyRootFilesystem = !*desired
						require.Equal(t, original, body, "Pod must not alias the rule")
						require.Equal(t, desired, pod.Spec.InitContainers[0].SecurityContext.ReadOnlyRootFilesystem, "containers must not share the Boolean")
					}
				})
			}
		}
	}
}

func TestReadOnlyRootFilesystemTargets(t *testing.T) {
	for _, tc := range []struct {
		name                     string
		targets                  []rules.WorkloadValidationTarget
		regular, init, ephemeral bool
	}{
		{"omitted", nil, true, true, true},
		{"empty", []rules.WorkloadValidationTarget{}, true, true, true},
		{"pod", []rules.WorkloadValidationTarget{rules.ValidatePod}, true, true, true},
		{"regular", []rules.WorkloadValidationTarget{rules.ValidateContainers}, true, false, false},
		{"init", []rules.WorkloadValidationTarget{rules.ValidateInitContainers}, false, true, false},
		{"ephemeral", []rules.WorkloadValidationTarget{rules.ValidateEphemeralContainers}, false, false, true},
		{"combined", []rules.WorkloadValidationTarget{rules.ValidateContainers, rules.ValidateInitContainers}, true, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pod := rootFilesystemPod(nil)
			pod.Spec.Containers[0].SecurityContext = nil
			body := rootFilesystemBody(new(true), tc.targets...)
			_, err := MutatePodPlacement(t.Context(), pod, []*rules.NamespaceRuleBodyNamespace{body}, nil)
			require.NoError(t, err)
			for i, context := range []*corev1.SecurityContext{pod.Spec.Containers[0].SecurityContext, pod.Spec.InitContainers[0].SecurityContext, pod.Spec.EphemeralContainers[0].SecurityContext} {
				if []bool{tc.regular, tc.init, tc.ephemeral}[i] {
					require.Equal(t, new(true), rootFilesystemValue(context))
				} else {
					require.Nil(t, rootFilesystemValue(context))
				}
			}
			require.Equal(t, rootFilesystemValue(pod.Spec.InitContainers[0].SecurityContext), rootFilesystemValue(pod.Spec.InitContainers[1].SecurityContext), "native sidecars are init containers")
		})
	}
}

func TestReadOnlyRootFilesystemConditionsAndIsolation(t *testing.T) {
	for _, windows := range []bool{false, true} {
		pod := rootFilesystemPod(new(false))
		if windows {
			pod.Spec.OS = &corev1.PodOS{Name: corev1.Windows}
		}
		body := rootFilesystemBody(new(true), rules.ValidatePod)
		body.Mutate = append(body.Mutate,
			rules.NamespaceRuleMutation{Conditions: []rules.AdmissionCondition{{Expression: "object.spec.containers[0].securityContext.readOnlyRootFilesystem == true"}}, Workloads: rules.WorkloadMutation{NodeSelector: map[string]string{"observed": "yes"}}},
			rules.NamespaceRuleMutation{Conditions: []rules.AdmissionCondition{{Expression: "false"}}, Workloads: rules.WorkloadMutation{ReadOnlyRootFilesystem: new(false)}},
		)
		before := pod.DeepCopy()
		changed, err := MutatePodPlacement(t.Context(), pod, []*rules.NamespaceRuleBodyNamespace{nil, body}, mutationConditions(t))
		require.NoError(t, err)
		require.Equal(t, !windows, changed)
		if windows {
			require.Equal(t, before, pod)
		} else {
			require.Equal(t, "yes", pod.Spec.NodeSelector["observed"])
		}
	}
	// Conflicting ordered rules can restore the original value without a patch.
	pod := rootFilesystemPod(new(false))
	changed, err := MutatePodPlacement(t.Context(), pod, []*rules.NamespaceRuleBodyNamespace{rootFilesystemBody(new(true)), rootFilesystemBody(new(false))}, nil)
	require.NoError(t, err)
	require.False(t, changed)
	// A second namespace profile must not reuse results or mutate the first rule.
	other := rootFilesystemPod(nil)
	changed, err = MutatePodPlacement(t.Context(), other, []*rules.NamespaceRuleBodyNamespace{rootFilesystemBody(new(false))}, nil)
	require.NoError(t, err)
	require.True(t, changed)
	require.Equal(t, new(false), other.Spec.Containers[0].SecurityContext.ReadOnlyRootFilesystem)
}
