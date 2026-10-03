// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package mutation

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/projectcapsule/capsule/internal/cache"
	"github.com/projectcapsule/capsule/pkg/api/rules"
)

func imagePullPolicyBody(value corev1.PullPolicy, targets ...rules.WorkloadValidationTarget) *rules.NamespaceRuleBodyNamespace {
	return &rules.NamespaceRuleBodyNamespace{Mutate: []rules.NamespaceRuleMutation{{Workloads: rules.WorkloadMutation{Registries: rules.WorkloadRegistryMutation{ImagePullPolicy: value}, Targets: targets}}}}
}

func TestImagePullPolicyMutationValuesAndActions(t *testing.T) {
	for _, action := range []rules.MutationAction{"", rules.MutationActionMerge, rules.MutationActionReplace} {
		for _, initial := range []corev1.PullPolicy{"", corev1.PullAlways, corev1.PullIfNotPresent, corev1.PullNever} {
			for _, desired := range []corev1.PullPolicy{"", corev1.PullAlways, corev1.PullIfNotPresent, corev1.PullNever} {
				t.Run(fmt.Sprintf("%s/%s/%s", action, initial, desired), func(t *testing.T) {
					pod := rootFilesystemPod(nil)
					for _, container := range workloadMutationContainers(pod, false, nil) {
						*container.pullPolicy = initial
						*container.context = nil
					}
					before := pod.DeepCopy()
					body := imagePullPolicyBody(desired)
					body.Mutate[0].Action = action
					original := body.DeepCopy()
					want := initial
					if desired != "" {
						want = desired
					}
					for pass := range 2 {
						changed, err := MutatePodPlacement(t.Context(), pod, []*rules.NamespaceRuleBodyNamespace{body}, nil)
						require.NoError(t, err)
						require.Equal(t, pass == 0 && initial != want, changed)
						for _, container := range workloadMutationContainers(pod, false, nil) {
							require.Equal(t, want, *container.pullPolicy)
							require.Nil(t, *container.context, "image policy does not allocate a security context")
						}
					}
					for _, container := range workloadMutationContainers(before, false, nil) {
						*container.pullPolicy = want
					}
					require.Equal(t, before, pod, "unrelated container and Pod fields are preserved")
					require.Equal(t, original, body, "mutation never modifies rule/cache-owned values")
				})
			}
		}
	}
}

func TestImagePullPolicyMutationTargets(t *testing.T) {
	for _, targets := range [][]rules.WorkloadValidationTarget{
		nil, {}, {rules.ValidatePod}, {rules.ValidateContainers}, {rules.ValidateInitContainers},
		{rules.ValidateEphemeralContainers}, {rules.ValidateContainers, rules.ValidateInitContainers},
	} {
		for _, windows := range []bool{false, true} {
			t.Run(fmt.Sprintf("%v/windows=%t", targets, windows), func(t *testing.T) {
				pod := rootFilesystemPod(nil)
				if windows {
					pod.Spec.OS = &corev1.PodOS{Name: corev1.Windows}
				}
				body := imagePullPolicyBody(corev1.PullAlways, targets...)
				body.Mutate[0].Workloads.Security.ReadOnlyRootFilesystem = new(true)
				changed, err := MutatePodPlacement(t.Context(), pod, []*rules.NamespaceRuleBodyNamespace{body}, nil)
				require.NoError(t, err)
				require.True(t, changed)
				selected := map[rules.WorkloadValidationTarget]bool{}
				for _, target := range targets {
					selected[target] = true
				}
				for _, container := range workloadMutationContainers(pod, false, nil) {
					want := corev1.PullPolicy("")
					if len(targets) == 0 || selected[rules.ValidatePod] || selected[container.target] {
						want = corev1.PullAlways
					}
					require.Equal(t, want, *container.pullPolicy)
					if windows || want == "" {
						require.Nil(t, rootFilesystemValue(*container.context))
					} else {
						require.Equal(t, new(true), rootFilesystemValue(*container.context))
					}
				}
			})
		}
	}
}

func TestImagePullPolicyMutationOrderingAndConditions(t *testing.T) {
	pod := rootFilesystemPod(nil)
	body := imagePullPolicyBody(corev1.PullAlways)
	body.Mutate = append(body.Mutate,
		rules.NamespaceRuleMutation{Conditions: []rules.AdmissionCondition{{Expression: "object.spec.containers[0].imagePullPolicy == 'Always'"}}, Workloads: rules.WorkloadMutation{Registries: rules.WorkloadRegistryMutation{ImagePullPolicy: corev1.PullNever}}},
		rules.NamespaceRuleMutation{Conditions: []rules.AdmissionCondition{{Expression: "false"}}, Workloads: rules.WorkloadMutation{Registries: rules.WorkloadRegistryMutation{ImagePullPolicy: corev1.PullIfNotPresent}}},
	)
	changed, err := MutatePodPlacement(t.Context(), pod, []*rules.NamespaceRuleBodyNamespace{nil, body}, mutationConditions(t))
	require.NoError(t, err)
	require.True(t, changed)
	require.Equal(t, corev1.PullNever, pod.Spec.Containers[0].ImagePullPolicy)
	// Later rules can restore the original value without emitting a patch.
	changed, err = MutatePodPlacement(t.Context(), pod, []*rules.NamespaceRuleBodyNamespace{imagePullPolicyBody(corev1.PullAlways), imagePullPolicyBody(corev1.PullNever)}, nil)
	require.NoError(t, err)
	require.False(t, changed)
	pod.Spec.EphemeralContainers = nil
	before := pod.DeepCopy()
	body = imagePullPolicyBody(corev1.PullAlways, rules.ValidateEphemeralContainers)
	body.Mutate[0].Conditions = []rules.AdmissionCondition{{Expression: "object.spec.ephemeralContainers[0].name == 'debug'"}}
	changed, err = MutatePodPlacement(t.Context(), pod, []*rules.NamespaceRuleBodyNamespace{body}, mutationConditions(t))
	require.NoError(t, err, "skip incompatible targets before evaluating their conditions")
	require.False(t, changed)
	require.Equal(t, before, pod)
}

func TestImagePullPolicyAdmissionScope(t *testing.T) {
	compiler, err := cache.NewCELCache()
	require.NoError(t, err)
	for _, tc := range []struct {
		name, subresource, condition         string
		operation                            admissionv1.Operation
		windows, missingOld, changed, failed bool
	}{
		{"create", "", "", admissionv1.Create, false, true, true, false},
		{"ephemeral", "ephemeralcontainers", "true", admissionv1.Update, false, false, true, false},
		{"Windows create", "", "", admissionv1.Create, true, true, true, false},
		{"Windows ephemeral", "ephemeralcontainers", "", admissionv1.Update, true, false, true, false},
		{"main update", "", "object.spec.missing == true", admissionv1.Update, false, false, false, false},
		{"status", "status", "object.spec.missing == true", admissionv1.Update, false, false, false, false},
		{"resize", "resize", "object.spec.missing == true", admissionv1.Update, false, false, false, false},
		{"condition false", "ephemeralcontainers", "false", admissionv1.Update, false, false, false, false},
		{"condition error", "ephemeralcontainers", "object.spec.missing == true", admissionv1.Update, false, false, false, true},
		{"old required", "ephemeralcontainers", "", admissionv1.Update, false, true, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			old, obj := ephemeralRootFilesystemObjects(t)
			if tc.missingOld {
				old = nil
			}
			if tc.windows {
				require.NoError(t, unstructured.SetNestedField(obj.Object, "windows", "spec", "os", "name"))
			}
			before := obj.DeepCopy()
			body := imagePullPolicyBody(corev1.PullAlways)
			if tc.condition != "" {
				body.Mutate[0].Conditions = []rules.AdmissionCondition{{Expression: tc.condition}}
			}
			req := rootFilesystemAdmissionRequest(t, obj, old, tc.operation, tc.subresource)
			response := MetadataRules(compiler).OnUpdate(nil, nil, old, obj, nil, nil, nil, []*rules.NamespaceRuleBodyNamespace{body})(t.Context(), req)
			if tc.failed {
				require.NotNil(t, response)
				require.False(t, response.Allowed)
			} else if tc.changed {
				require.NotNil(t, response)
				require.True(t, response.Allowed)
				require.NotEmpty(t, response.Patches)
			} else {
				require.Nil(t, response)
			}
			if !tc.changed {
				require.Equal(t, before, obj)
			}
		})
	}
}

func TestImagePullPolicyEphemeralPreservesExistingAndUnknownFields(t *testing.T) {
	for _, action := range []rules.MutationAction{rules.MutationActionMerge, rules.MutationActionReplace} {
		old, obj := ephemeralRootFilesystemObjects(t)
		items := obj.Object["spec"].(map[string]any)["ephemeralContainers"].([]any)
		items[1].(map[string]any)["futureContainerField"] = "keep"
		items[1].(map[string]any)["imagePullPolicy"] = "Never"
		before, oldBefore := obj.DeepCopy(), old.DeepCopy()
		body := imagePullPolicyBody(corev1.PullAlways, rules.ValidateEphemeralContainers)
		body.Mutate[0].Action = action
		body.Mutate = append(body.Mutate, rules.NamespaceRuleMutation{
			Conditions: []rules.AdmissionCondition{{Expression: "object.spec.ephemeralContainers[1].imagePullPolicy == 'Always'"}},
			Workloads:  rules.WorkloadMutation{Registries: rules.WorkloadRegistryMutation{ImagePullPolicy: corev1.PullIfNotPresent}, Security: rules.WorkloadSecurityMutation{ReadOnlyRootFilesystem: new(true)}},
		})
		original := body.DeepCopy()
		for pass := range 2 {
			req := rootFilesystemAdmissionRequest(t, obj, old, admissionv1.Update, "ephemeralcontainers")
			compiler, err := cache.NewCELCache()
			require.NoError(t, err)
			response := MetadataRules(compiler).OnUpdate(nil, nil, old, obj, nil, nil, nil, []*rules.NamespaceRuleBodyNamespace{body})(t.Context(), req)
			if pass == 0 {
				require.NotNil(t, response)
				require.True(t, response.Allowed)
				require.Len(t, response.Patches, 2)
				item := before.Object["spec"].(map[string]any)["ephemeralContainers"].([]any)[1].(map[string]any)
				item["imagePullPolicy"] = "IfNotPresent"
				item["securityContext"] = map[string]any{"readOnlyRootFilesystem": true}
			} else {
				require.Nil(t, response)
			}
			require.Equal(t, before, obj, "only the new container's configured leaves change")
			require.Equal(t, oldBefore, old)
			require.Equal(t, original, body)
		}
	}
}

func TestRegistryMutationSkipsControllerTemplates(t *testing.T) {
	obj := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "apps/v1", "kind": "Deployment",
		"metadata": map[string]any{"name": "app", "namespace": "tenant-a"},
		"spec": map[string]any{"template": map[string]any{"spec": map[string]any{
			"containers": []any{map[string]any{"name": "app", "image": "registry.k8s.io/pause:3.10", "imagePullPolicy": "Never"}},
		}}},
	}}
	before := obj.DeepCopy()
	for _, operation := range []admissionv1.Operation{admissionv1.Create, admissionv1.Update} {
		req := rootFilesystemAdmissionRequest(t, obj, before, operation, "")
		req.Kind.Group, req.Kind.Kind = "apps", "Deployment"
		req.Resource.Group, req.Resource.Resource = "apps", "deployments"
		body := imagePullPolicyBody(corev1.PullAlways)
		body.Mutate[0].Workloads.Registries.ImagePullSecrets = pullSecretRefs("pull")
		response := MetadataRules(nil).OnUpdate(nil, nil, before, obj, nil, nil, nil, []*rules.NamespaceRuleBodyNamespace{body})(t.Context(), req)
		require.Nil(t, response)
		require.Equal(t, before, obj)
	}
}
