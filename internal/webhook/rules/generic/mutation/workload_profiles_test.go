// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package mutation

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	"github.com/projectcapsule/capsule/internal/cache"
	"github.com/projectcapsule/capsule/pkg/api/rules"
)

func TestSecurityProfileMutation(t *testing.T) {
	for _, action := range []rules.MutationAction{"", rules.MutationActionMerge, rules.MutationActionReplace} {
		for _, initial := range []string{"absent", "RuntimeDefault", "Localhost", "Unconfined"} {
			for _, windows := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%s/windows=%v", action, initial, windows), func(t *testing.T) {
					desired := rules.WorkloadMutation{Security: rules.WorkloadSecurityMutation{SeccompProfile: &corev1.SeccompProfile{Type: corev1.SeccompProfileTypeRuntimeDefault}, AppArmorProfile: &corev1.AppArmorProfile{Type: corev1.AppArmorProfileTypeRuntimeDefault}}}
					body := &rules.NamespaceRuleBodyNamespace{Mutate: []rules.NamespaceRuleMutation{{Action: action, Workloads: desired}}}
					original := body.DeepCopy()
					pod := &corev1.Pod{Spec: corev1.PodSpec{SecurityContext: &corev1.PodSecurityContext{RunAsUser: new(int64(1000))}, Containers: []corev1.Container{{Name: "app", SecurityContext: &corev1.SecurityContext{SeccompProfile: &corev1.SeccompProfile{Type: corev1.SeccompProfileTypeUnconfined}}}}}}
					if initial != "absent" {
						pod.Spec.SecurityContext.SeccompProfile = &corev1.SeccompProfile{Type: corev1.SeccompProfileType(initial)}
						pod.Spec.SecurityContext.AppArmorProfile = &corev1.AppArmorProfile{Type: corev1.AppArmorProfileType(initial)}
						if initial == "Localhost" {
							pod.Spec.SecurityContext.SeccompProfile.LocalhostProfile = new("profiles/old.json")
							pod.Spec.SecurityContext.AppArmorProfile.LocalhostProfile = new("old-profile")
						}
					}
					if windows {
						pod.Spec.OS = &corev1.PodOS{Name: corev1.Windows}
					}
					before := pod.DeepCopy()
					change := !windows && (initial == "absent" || (action == rules.MutationActionReplace && initial != "RuntimeDefault"))
					for pass := range 2 {
						changed, err := MutatePodPlacement(t.Context(), pod, []*rules.NamespaceRuleBodyNamespace{body}, nil)
						require.NoError(t, err)
						require.Equal(t, change && pass == 0, changed)
					}
					if change {
						require.Equal(t, desired.Security.SeccompProfile, pod.Spec.SecurityContext.SeccompProfile)
						require.Equal(t, desired.Security.AppArmorProfile, pod.Spec.SecurityContext.AppArmorProfile)
					} else {
						require.Equal(t, before, pod)
					}
					require.Equal(t, before.Spec.Containers, pod.Spec.Containers)
					require.Equal(t, int64(1000), *pod.Spec.SecurityContext.RunAsUser)
					require.Equal(t, original, body)
					if pod.Spec.SecurityContext.SeccompProfile != nil {
						pod.Spec.SecurityContext.SeccompProfile.Type = corev1.SeccompProfileTypeUnconfined
					}
					require.Equal(t, original, body, "mutated Pod must not alias rules")
				})
			}
		}
	}
}

func TestSecurityProfileMutationConditionsAndScope(t *testing.T) {
	body := &rules.NamespaceRuleBodyNamespace{Mutate: []rules.NamespaceRuleMutation{
		{Workloads: rules.WorkloadMutation{Security: rules.WorkloadSecurityMutation{SeccompProfile: &corev1.SeccompProfile{Type: corev1.SeccompProfileTypeRuntimeDefault}}}},
		{Conditions: []rules.AdmissionCondition{{Expression: `object.spec.securityContext.seccompProfile.type == 'RuntimeDefault'`}}, Workloads: rules.WorkloadMutation{Security: rules.WorkloadSecurityMutation{AppArmorProfile: &corev1.AppArmorProfile{Type: corev1.AppArmorProfileTypeLocalhost, LocalhostProfile: new("team-profile")}}}},
		{Action: rules.MutationActionReplace, Conditions: []rules.AdmissionCondition{{Expression: "false"}}, Workloads: rules.WorkloadMutation{Security: rules.WorkloadSecurityMutation{SeccompProfile: &corev1.SeccompProfile{Type: corev1.SeccompProfileTypeUnconfined}}}},
	}}
	compiler, err := cache.NewCELCache()
	require.NoError(t, err)
	h := MetadataRules(compiler)
	for _, tc := range []struct {
		op        admissionv1.Operation
		kind, sub string
		changed   bool
	}{
		{admissionv1.Create, "Pod", "", true}, {admissionv1.Update, "Pod", "", false}, {admissionv1.Update, "Pod", "ephemeralcontainers", false}, {admissionv1.Create, "Service", "", false}, {admissionv1.Create, "Deployment", "", false},
	} {
		t.Run(fmt.Sprintf("%s/%s/%s", tc.op, tc.kind, tc.sub), func(t *testing.T) {
			obj := &unstructured.Unstructured{Object: map[string]any{"apiVersion": "v1", "kind": tc.kind, "spec": map[string]any{}}}
			raw, err := json.Marshal(obj)
			require.NoError(t, err)
			req := admission.Request{AdmissionRequest: admissionv1.AdmissionRequest{Operation: tc.op, Kind: metav1.GroupVersionKind{Version: "v1", Kind: tc.kind}, SubResource: tc.sub, Object: runtime.RawExtension{Raw: raw}}}
			before := obj.DeepCopy()
			response := h.OnCreate(nil, nil, obj, nil, nil, nil, []*rules.NamespaceRuleBodyNamespace{body})(t.Context(), req)
			if tc.changed {
				require.NotNil(t, response)
				require.True(t, response.Allowed)
				require.NotEmpty(t, response.Patches)
				value, _, err := unstructured.NestedString(obj.Object, "spec", "securityContext", "appArmorProfile", "localhostProfile")
				require.NoError(t, err)
				require.Equal(t, "team-profile", value)
			} else {
				require.Nil(t, response)
				require.Equal(t, before, obj)
			}
		})
	}
}

func BenchmarkSecurityProfileMutation(b *testing.B) {
	for _, size := range []int{1, 20} {
		for _, action := range []rules.MutationAction{rules.MutationActionMerge, rules.MutationActionReplace} {
			b.Run(fmt.Sprintf("rules=%d/%s", size, action), func(b *testing.B) {
				var bodies []*rules.NamespaceRuleBodyNamespace
				for range size {
					bodies = append(bodies, &rules.NamespaceRuleBodyNamespace{Mutate: []rules.NamespaceRuleMutation{{Action: action, Workloads: rules.WorkloadMutation{Security: rules.WorkloadSecurityMutation{SeccompProfile: &corev1.SeccompProfile{Type: corev1.SeccompProfileTypeRuntimeDefault}, AppArmorProfile: &corev1.AppArmorProfile{Type: corev1.AppArmorProfileTypeRuntimeDefault}}}}}})
				}
				obj := &unstructured.Unstructured{}
				req := admission.Request{AdmissionRequest: admissionv1.AdmissionRequest{Operation: admissionv1.Create, Kind: metav1.GroupVersionKind{Version: "v1", Kind: "Pod"}, Object: runtime.RawExtension{Raw: []byte(`{"apiVersion":"v1","kind":"Pod","spec":{}}`)}}}
				call := MetadataRules(nil).OnCreate(nil, nil, obj, nil, nil, nil, bodies)
				b.ReportAllocs()
				for b.Loop() {
					obj.Object = map[string]any{"apiVersion": "v1", "kind": "Pod", "spec": map[string]any{}}
					response := call(b.Context(), req)
					if response == nil || !response.Allowed || len(response.Patches) == 0 {
						b.Fatal("profile mutation failed")
					}
				}
			})
		}
	}
}
