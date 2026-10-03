// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package mutation

import (
	"fmt"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"
	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/projectcapsule/capsule/internal/cache"
	"github.com/projectcapsule/capsule/pkg/api/rules"
)

func pullSecretRefs(names ...string) []corev1.LocalObjectReference {
	refs := make([]corev1.LocalObjectReference, len(names))
	for i, name := range names {
		refs[i].Name = name
	}
	return refs
}

func imagePullSecretsBody(action rules.MutationAction, secrets []corev1.LocalObjectReference, targets ...rules.WorkloadValidationTarget) *rules.NamespaceRuleBodyNamespace {
	return &rules.NamespaceRuleBodyNamespace{Mutate: []rules.NamespaceRuleMutation{{Action: action, Workloads: rules.WorkloadMutation{Targets: targets, Registries: rules.WorkloadRegistryMutation{ImagePullSecrets: secrets}}}}}
}

func TestImagePullSecretsMutationActionsAndPresence(t *testing.T) {
	for _, action := range []rules.MutationAction{"", rules.MutationActionMerge, rules.MutationActionReplace} {
		for _, tc := range []struct {
			name                             string
			input, desired, merged, replaced []corev1.LocalObjectReference
		}{
			{"omitted", pullSecretRefs("user"), nil, pullSecretRefs("user"), pullSecretRefs("user")},
			{"empty", pullSecretRefs("user"), pullSecretRefs(), pullSecretRefs("user"), pullSecretRefs()},
			{"initially absent", nil, pullSecretRefs("tenant"), pullSecretRefs("tenant"), pullSecretRefs("tenant")},
			{"already present", pullSecretRefs("tenant"), pullSecretRefs("tenant"), pullSecretRefs("tenant"), pullSecretRefs("tenant")},
			{"preserve order and deduplicate", pullSecretRefs("user", "shared"), pullSecretRefs("shared", "tenant"), pullSecretRefs("user", "shared", "tenant"), pullSecretRefs("shared", "tenant")},
			{"existing duplicates", pullSecretRefs("user", "shared", "user", "shared"), pullSecretRefs("shared", "tenant"), pullSecretRefs("user", "shared", "tenant"), pullSecretRefs("shared", "tenant")},
			{"same length after deduplication and append", pullSecretRefs("user", "user"), pullSecretRefs("tenant"), pullSecretRefs("user", "tenant"), pullSecretRefs("tenant")},
			{"only deduplication changes", pullSecretRefs("user", "user", "shared"), pullSecretRefs("shared"), pullSecretRefs("user", "shared"), pullSecretRefs("shared")},
			{"empty deduplicates", pullSecretRefs("user", "user"), pullSecretRefs(), pullSecretRefs("user"), pullSecretRefs()},
			{"omitted preserves duplicates", pullSecretRefs("user", "user"), nil, pullSecretRefs("user", "user"), pullSecretRefs("user", "user")},
			{"empty without existing", nil, pullSecretRefs(), nil, pullSecretRefs()},
		} {
			t.Run(fmt.Sprintf("%s/%s", action, tc.name), func(t *testing.T) {
				pod := rootFilesystemPod(nil)
				pod.Spec.ImagePullSecrets = slices.Clone(tc.input)
				before := pod.DeepCopy()
				body := imagePullSecretsBody(action, tc.desired)
				original := body.DeepCopy()
				want := tc.merged
				if action == rules.MutationActionReplace {
					want = tc.replaced
				}
				for pass := range 2 {
					changed, err := MutatePodPlacement(t.Context(), pod, []*rules.NamespaceRuleBodyNamespace{body}, nil)
					require.NoError(t, err)
					require.Equal(t, pass == 0 && !slices.Equal(tc.input, want), changed)
					require.Equal(t, want, pod.Spec.ImagePullSecrets)
				}
				before.Spec.ImagePullSecrets = slices.Clone(want)
				require.Equal(t, before, pod, "other Pod and container fields are unchanged")
				if len(pod.Spec.ImagePullSecrets) > 0 {
					pod.Spec.ImagePullSecrets[0].Name = "changed-by-caller"
				}
				require.Equal(t, original, body, "the Pod must not alias rule-owned reference slices")
			})
		}
	}
}

func TestImagePullSecretsDeduplicationAcrossRules(t *testing.T) {
	pod := rootFilesystemPod(nil)
	pod.Spec.ImagePullSecrets = pullSecretRefs("user", "shared", "user")
	originalRefs := pod.Spec.ImagePullSecrets
	bodies := []*rules.NamespaceRuleBodyNamespace{
		imagePullSecretsBody(rules.MutationActionMerge, pullSecretRefs("shared", "tenant")),
		imagePullSecretsBody(rules.MutationActionMerge, pullSecretRefs("user", "tenant", "another")),
	}
	bodies[1].Mutate[0].Conditions = []rules.AdmissionCondition{{Expression: "object.spec.imagePullSecrets.size() == 3"}}
	for pass := range 2 {
		changed, err := MutatePodPlacement(t.Context(), pod, bodies, mutationConditions(t))
		require.NoError(t, err)
		require.Equal(t, pass == 0, changed)
		require.Equal(t, pullSecretRefs("user", "shared", "tenant", "another"), pod.Spec.ImagePullSecrets)
	}
	require.Equal(t, pullSecretRefs("user", "shared", "user"), originalRefs, "deduplication must not overwrite the original snapshot")
}

func TestImagePullSecretsMutationTargetsAndOS(t *testing.T) {
	for _, targets := range [][]rules.WorkloadValidationTarget{nil, {}, {rules.ValidatePod}, {rules.ValidateContainers}, {rules.ValidateInitContainers}, {rules.ValidateEphemeralContainers}} {
		for _, os := range []corev1.OSName{corev1.Linux, corev1.Windows} {
			t.Run(fmt.Sprintf("%v/%s", targets, os), func(t *testing.T) {
				pod := rootFilesystemPod(nil)
				pod.Spec.OS = &corev1.PodOS{Name: os}
				body := imagePullSecretsBody(rules.MutationActionMerge, pullSecretRefs("pull"), targets...)
				want := len(targets) == 0 || slices.Contains(targets, rules.ValidatePod)
				if !want {
					body.Mutate[0].Conditions = []rules.AdmissionCondition{{Expression: "object.missing == true"}}
				}
				changed, err := MutatePodPlacement(t.Context(), pod, []*rules.NamespaceRuleBodyNamespace{body}, mutationConditions(t))
				require.NoError(t, err)
				require.Equal(t, want, changed)
				if want {
					require.Equal(t, pullSecretRefs("pull"), pod.Spec.ImagePullSecrets)
				} else {
					require.Empty(t, pod.Spec.ImagePullSecrets)
				}
			})
		}
	}
}

func TestImagePullSecretsOrderingAndConditions(t *testing.T) {
	pod := rootFilesystemPod(nil)
	pod.Spec.ImagePullSecrets = pullSecretRefs("original")
	body := imagePullSecretsBody(rules.MutationActionMerge, pullSecretRefs("tenant"))
	later := imagePullSecretsBody(rules.MutationActionReplace, pullSecretRefs("selected"))
	later.Mutate[0].Conditions = []rules.AdmissionCondition{{Expression: "object.spec.imagePullSecrets.exists(s, s.name == 'tenant')"}}
	skipped := imagePullSecretsBody(rules.MutationActionReplace, pullSecretRefs())
	skipped.Mutate[0].Conditions = []rules.AdmissionCondition{{Expression: "false"}}
	changed, err := MutatePodPlacement(t.Context(), pod, []*rules.NamespaceRuleBodyNamespace{nil, body, later, skipped}, mutationConditions(t))
	require.NoError(t, err)
	require.True(t, changed)
	require.Equal(t, pullSecretRefs("selected"), pod.Spec.ImagePullSecrets)
	changed, err = MutatePodPlacement(t.Context(), pod, []*rules.NamespaceRuleBodyNamespace{body, imagePullSecretsBody(rules.MutationActionReplace, pullSecretRefs("selected"))}, nil)
	require.NoError(t, err)
	require.False(t, changed, "rules restoring the original list do not emit a patch")
}

func TestImagePullSecretsAdmissionScope(t *testing.T) {
	compiler, err := cache.NewCELCache()
	require.NoError(t, err)
	for _, tc := range []struct {
		name, subresource, expression string
		operation                     admissionv1.Operation
		clear, changed, failed        bool
	}{
		{"create", "", "", admissionv1.Create, false, true, false},
		{"clear", "", "", admissionv1.Create, true, true, false},
		{"condition false", "", "false", admissionv1.Create, false, false, false},
		{"condition error", "", "object.missing == true", admissionv1.Create, false, false, true},
		{"main update", "", "object.missing == true", admissionv1.Update, false, false, false},
		{"ephemeral update", "ephemeralcontainers", "object.missing == true", admissionv1.Update, false, false, false},
		{"status", "status", "object.missing == true", admissionv1.Update, false, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			old, obj := ephemeralRootFilesystemObjects(t)
			require.NoError(t, unstructured.SetNestedSlice(obj.Object, []any{map[string]any{"name": "user"}}, "spec", "imagePullSecrets"))
			before := obj.DeepCopy()
			desired := pullSecretRefs("tenant")
			if tc.clear {
				desired = pullSecretRefs()
			}
			body := imagePullSecretsBody(rules.MutationActionReplace, desired)
			if tc.expression != "" {
				body.Mutate[0].Conditions = []rules.AdmissionCondition{{Expression: tc.expression}}
			}
			req := rootFilesystemAdmissionRequest(t, obj, old, tc.operation, tc.subresource)
			response := MetadataRules(compiler).OnUpdate(nil, nil, old, obj, nil, nil, nil, []*rules.NamespaceRuleBodyNamespace{body})(t.Context(), req)
			if tc.failed {
				require.NotNil(t, response)
				require.False(t, response.Allowed)
			} else if tc.changed {
				require.NotNil(t, response)
				require.True(t, response.Allowed)
				if tc.clear {
					require.Contains(t, fmt.Sprint(response.Patches), "/spec/imagePullSecrets")
					unstructured.RemoveNestedField(before.Object, "spec", "imagePullSecrets")
				} else {
					require.NoError(t, unstructured.SetNestedSlice(before.Object, []any{map[string]any{"name": "tenant"}}, "spec", "imagePullSecrets"))
				}
			} else {
				require.Nil(t, response)
			}
			require.Equal(t, before, obj)
		})
	}
}

func TestImagePullSecretsPreservedDuringEphemeralPolicyMutation(t *testing.T) {
	old, obj := ephemeralRootFilesystemObjects(t)
	require.NoError(t, unstructured.SetNestedSlice(obj.Object, []any{map[string]any{"name": "user"}}, "spec", "imagePullSecrets"))
	before := obj.DeepCopy()
	body := imagePullSecretsBody(rules.MutationActionReplace, pullSecretRefs("replacement"))
	body.Mutate[0].Workloads.Registries.ImagePullPolicy = corev1.PullAlways
	req := rootFilesystemAdmissionRequest(t, obj, old, admissionv1.Update, "ephemeralcontainers")
	response := MetadataRules(nil).OnUpdate(nil, nil, old, obj, nil, nil, nil, []*rules.NamespaceRuleBodyNamespace{body})(t.Context(), req)
	require.NotNil(t, response)
	require.True(t, response.Allowed)
	require.Len(t, response.Patches, 1)
	require.Equal(t, "/spec/ephemeralContainers/1/imagePullPolicy", response.Patches[0].Path)
	before.Object["spec"].(map[string]any)["ephemeralContainers"].([]any)[1].(map[string]any)["imagePullPolicy"] = "Always"
	require.Equal(t, before, obj)
}
