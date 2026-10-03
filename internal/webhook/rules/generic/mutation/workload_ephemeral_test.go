// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package mutation

import (
	"encoding/json"
	"os"
	"regexp"
	"testing"

	"github.com/stretchr/testify/require"
	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apiserver/pkg/cel/environment"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	"github.com/projectcapsule/capsule/internal/cache"
	"github.com/projectcapsule/capsule/pkg/api/rules"
)

func ephemeralRootFilesystemObjects(t testing.TB) (*unstructured.Unstructured, *unstructured.Unstructured) {
	t.Helper()
	pod := rootFilesystemPod(new(false))
	pod.TypeMeta = metav1.TypeMeta{APIVersion: "v1", Kind: "Pod"}
	pod.Name, pod.Namespace = "app", "tenant-a"
	value, err := runtime.DefaultUnstructuredConverter.ToUnstructured(pod)
	require.NoError(t, err)
	old := &unstructured.Unstructured{Object: value}
	pod.Spec.EphemeralContainers = append(pod.Spec.EphemeralContainers, corev1.EphemeralContainer{EphemeralContainerCommon: corev1.EphemeralContainerCommon{Name: "new-debug", Image: "example.com/debug"}})
	value, err = runtime.DefaultUnstructuredConverter.ToUnstructured(pod)
	require.NoError(t, err)
	return old, &unstructured.Unstructured{Object: value}
}

func rootFilesystemAdmissionRequest(t testing.TB, obj, old *unstructured.Unstructured, operation admissionv1.Operation, subresource string) admission.Request {
	t.Helper()
	raw, err := json.Marshal(obj)
	require.NoError(t, err)
	previous, err := json.Marshal(old)
	require.NoError(t, err)
	return admission.Request{AdmissionRequest: admissionv1.AdmissionRequest{Operation: operation, Kind: metav1.GroupVersionKind{Version: "v1", Kind: "Pod"}, Resource: metav1.GroupVersionResource{Version: "v1", Resource: "pods"}, Namespace: obj.GetNamespace(), SubResource: subresource, Object: runtime.RawExtension{Raw: raw}, OldObject: runtime.RawExtension{Raw: previous}}}
}

func TestEphemeralReadOnlyRootFilesystemMutation(t *testing.T) {
	for _, target := range []rules.WorkloadValidationTarget{"", rules.ValidatePod, rules.ValidateEphemeralContainers} {
		for _, desired := range []bool{false, true} {
			t.Run(string(target)+"/"+map[bool]string{true: "readonly", false: "writable"}[desired], func(t *testing.T) {
				old, obj := ephemeralRootFilesystemObjects(t)
				items := obj.Object["spec"].(map[string]any)["ephemeralContainers"].([]any)
				items[1].(map[string]any)["futureContainerField"] = "keep"
				items[1].(map[string]any)["securityContext"] = map[string]any{"futureSecurityField": "keep"}
				body := rootFilesystemBody(new(desired))
				if target != "" {
					body.Mutate[0].Workloads.Targets = []rules.WorkloadValidationTarget{target}
				}
				body.Mutate[0].Action = rules.MutationActionReplace
				original := body.DeepCopy()
				before, oldBefore := obj.DeepCopy(), old.DeepCopy()
				req := rootFilesystemAdmissionRequest(t, obj, old, admissionv1.Update, "ephemeralcontainers")
				response := MetadataRules(nil).OnUpdate(nil, nil, old, obj, nil, nil, nil, []*rules.NamespaceRuleBodyNamespace{body})(t.Context(), req)
				require.NotNil(t, response)
				require.True(t, response.Allowed)
				require.Len(t, response.Patches, 1)
				require.Equal(t, "/spec/ephemeralContainers/1/securityContext/readOnlyRootFilesystem", response.Patches[0].Path)
				beforeItems := before.Object["spec"].(map[string]any)["ephemeralContainers"].([]any)
				beforeItems[1].(map[string]any)["securityContext"].(map[string]any)["readOnlyRootFilesystem"] = desired
				require.Equal(t, before, obj, "only the selected Boolean may change")
				require.Equal(t, oldBefore, old)
				require.Equal(t, original, body)
				// Reinvocation retains the same old object and must produce no second patch.
				req = rootFilesystemAdmissionRequest(t, obj, old, admissionv1.Update, "ephemeralcontainers")
				require.Nil(t, MetadataRules(nil).OnUpdate(nil, nil, old, obj, nil, nil, nil, []*rules.NamespaceRuleBodyNamespace{body})(t.Context(), req))
			})
		}
	}
}

func TestRootFilesystemAdmissionScopeAndConditions(t *testing.T) {
	compiler, err := cache.NewCELCache()
	require.NoError(t, err)
	for _, tc := range []struct {
		name, subresource, expression        string
		target                               rules.WorkloadValidationTarget
		operation                            admissionv1.Operation
		windows, missingOld, changed, failed bool
	}{
		{"create", "", "", rules.ValidatePod, admissionv1.Create, false, true, true, false},
		{"ephemeral update", "ephemeralcontainers", "true", rules.ValidatePod, admissionv1.Update, false, false, true, false},
		{"other target skips invalid condition", "ephemeralcontainers", "object.spec.missing == 'x'", rules.ValidateContainers, admissionv1.Update, false, false, false, false},
		{"false condition", "ephemeralcontainers", "false", rules.ValidatePod, admissionv1.Update, false, false, false, false},
		{"error fails closed", "ephemeralcontainers", "object.spec.missing == 'x'", rules.ValidatePod, admissionv1.Update, false, false, false, true},
		{"old object required", "ephemeralcontainers", "", rules.ValidatePod, admissionv1.Update, false, true, false, true},
		{"Windows", "ephemeralcontainers", "object.spec.missing == 'x'", rules.ValidatePod, admissionv1.Update, true, false, false, false},
		{"main update", "", "object.spec.missing == 'x'", rules.ValidatePod, admissionv1.Update, false, false, false, false},
		{"status update", "status", "object.spec.missing == 'x'", rules.ValidatePod, admissionv1.Update, false, false, false, false},
		{"resize update", "resize", "object.spec.missing == 'x'", rules.ValidatePod, admissionv1.Update, false, false, false, false},
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
			body := rootFilesystemBody(new(true), tc.target)
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

func TestEphemeralMutationIgnoresPodPropertiesAndObservesPriorEntries(t *testing.T) {
	old, obj := ephemeralRootFilesystemObjects(t)
	body := rootFilesystemBody(new(true))
	body.Mutate[0].Workloads.Security.HostUsers = new(false)
	body.Mutate[0].Workloads.Security.SeccompProfile = &corev1.SeccompProfile{Type: corev1.SeccompProfileTypeRuntimeDefault}
	body.Mutate = append(body.Mutate, rules.NamespaceRuleMutation{Conditions: []rules.AdmissionCondition{{Expression: "object.spec.ephemeralContainers[1].securityContext.readOnlyRootFilesystem == true"}}, Workloads: rules.WorkloadMutation{Security: rules.WorkloadSecurityMutation{ReadOnlyRootFilesystem: new(false)}}})
	body.Mutate = append(body.Mutate, rules.NamespaceRuleMutation{Conditions: []rules.AdmissionCondition{{Expression: "object.spec.missing == 'x'"}}, Workloads: rules.WorkloadMutation{Placement: rules.WorkloadPlacementMutation{Scheduler: "irrelevant"}}})
	before := obj.DeepCopy()
	changed, err := mutateEphemeralRootFilesystems(t.Context(), obj, old, []*rules.NamespaceRuleBodyNamespace{body}, mutationConditions(t))
	require.NoError(t, err)
	require.True(t, changed)
	items := before.Object["spec"].(map[string]any)["ephemeralContainers"].([]any)
	items[1].(map[string]any)["securityContext"] = map[string]any{"readOnlyRootFilesystem": false}
	require.Equal(t, before, obj)
}

func TestMutationWebhookSubresourceSelection(t *testing.T) {
	data, err := os.ReadFile("../../../../../charts/capsule/templates/configuration.yaml")
	require.NoError(t, err)
	match := regexp.MustCompile(`(?m)name: mutations-supported-subresources\n\s+expression: '(.*)'`).FindSubmatch(data)
	require.Len(t, match, 2)
	compiler, err := cache.NewCELCache()
	require.NoError(t, err)
	compiled, err := compiler.GetOrCompileCondition(string(match[1]), environment.NewExpressions)
	require.NoError(t, err)
	for _, tc := range []struct {
		group, resource, sub, operation string
		allow                           bool
	}{
		{"", "pods", "", "CREATE", true}, {"", "services", "", "UPDATE", true},
		{"", "pods", "ephemeralcontainers", "UPDATE", true}, {"", "pods", "ephemeralcontainers", "CREATE", false},
		{"other.example.com", "pods", "ephemeralcontainers", "UPDATE", false}, {"", "services", "ephemeralcontainers", "UPDATE", false},
		{"", "pods", "status", "UPDATE", false}, {"", "pods", "resize", "UPDATE", false},
	} {
		got, err := compiled.EvaluateCondition(t.Context(), nil, map[string]any{"operation": tc.operation, "subResource": tc.sub, "resource": map[string]any{"group": tc.group, "resource": tc.resource}})
		require.NoError(t, err)
		require.Equal(t, tc.allow, got, "%+v", tc)
	}
}

func TestEphemeralMutationNoNewContainersAndMalformedObjects(t *testing.T) {
	for _, tc := range []struct {
		name      string
		edit      func(*unstructured.Unstructured, *unstructured.Unstructured)
		wantError string
	}{
		{"no new containers", func(old, obj *unstructured.Unstructured) { obj.Object = old.DeepCopy().Object }, ""},
		{"malformed new Pod", func(_, obj *unstructured.Unstructured) { obj.Object["spec"] = "invalid" }, "decode Pod"},
		{"malformed old array", func(old, _ *unstructured.Unstructured) {
			old.Object["spec"].(map[string]any)["ephemeralContainers"] = "invalid"
		}, "must be an array"},
		{"malformed old item", func(old, _ *unstructured.Unstructured) {
			old.Object["spec"].(map[string]any)["ephemeralContainers"] = []any{"invalid"}
		}, "must be an object"},
		{"missing old name", func(old, _ *unstructured.Unstructured) {
			old.Object["spec"].(map[string]any)["ephemeralContainers"] = []any{map[string]any{}}
		}, "must have a name"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			old, obj := ephemeralRootFilesystemObjects(t)
			tc.edit(old, obj)
			before := obj.DeepCopy()
			body := rootFilesystemBody(new(true))
			body.Mutate[0].Conditions = []rules.AdmissionCondition{{Expression: "object.invalid == true"}}
			changed, err := mutateEphemeralRootFilesystems(t.Context(), obj, old, []*rules.NamespaceRuleBodyNamespace{body}, mutationConditions(t))
			if tc.wantError == "" {
				require.NoError(t, err)
			} else {
				require.ErrorContains(t, err, tc.wantError)
			}
			require.False(t, changed)
			require.Equal(t, before, obj)
		})
	}
}

func TestEphemeralOnlyMutationSkipsCreateCondition(t *testing.T) {
	pod := rootFilesystemPod(nil)
	pod.Spec.EphemeralContainers = nil
	before := pod.DeepCopy()
	body := rootFilesystemBody(new(true), rules.ValidateEphemeralContainers)
	body.Mutate[0].Conditions = []rules.AdmissionCondition{{Expression: "object.spec.ephemeralContainers[0].name == 'debug'"}}
	changed, err := MutatePodPlacement(t.Context(), pod, []*rules.NamespaceRuleBodyNamespace{body}, mutationConditions(t))
	require.NoError(t, err)
	require.False(t, changed)
	require.Equal(t, before, pod)
}
