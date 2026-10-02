// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package ruleengine_test

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/require"
	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
	"sigs.k8s.io/yaml"

	capsule "github.com/projectcapsule/capsule/api/v1beta2"
	mutation "github.com/projectcapsule/capsule/internal/webhook/rules/generic/mutation"
	validation "github.com/projectcapsule/capsule/internal/webhook/rules/pods/validation"
	"github.com/projectcapsule/capsule/pkg/ruleengine"
	"github.com/projectcapsule/capsule/pkg/runtime/events"
	"github.com/projectcapsule/capsule/pkg/tenant"
)

func TestPlaygroundSecurityProfiles(t *testing.T) {
	data, err := os.ReadFile("../../playground/platform/tenants/solar.yaml")
	require.NoError(t, err)
	tnt := &capsule.Tenant{}
	require.NoError(t, yaml.UnmarshalStrict(data, tnt))
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))
	require.NoError(t, capsule.AddToScheme(scheme))
	for _, selected := range []bool{false, true} {
		for _, name := range []string{"default", "override-denied"} {
			ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "solar-test"}}
			if selected {
				ns.Labels = map[string]string{"security-profile": "confined", "apparmor": "enabled"}
			}
			bodies, err := tenant.BuildNamespaceRuleBodyStatus(scheme, ns, tnt)
			require.NoError(t, err)
			require.NoError(t, ruleengine.ValidateRuleStatusBody(nil, bodies, conditionCache(t)))
			data, err := os.ReadFile("../../playground/user/solar/security-profiles/" + name + ".yaml")
			require.NoError(t, err)
			obj := &unstructured.Unstructured{}
			require.NoError(t, yaml.Unmarshal(data, &obj.Object))
			raw, err := json.Marshal(obj)
			require.NoError(t, err)
			req := admission.Request{AdmissionRequest: admissionv1.AdmissionRequest{Operation: admissionv1.Create, Kind: metav1.GroupVersionKind{Version: "v1", Kind: "Pod"}, Object: runtime.RawExtension{Raw: raw}}}
			response := mutation.MetadataRules(nil).OnCreate(nil, nil, obj, nil, nil, tnt, bodies)(t.Context(), req)
			if response != nil {
				require.True(t, response.Allowed)
			}
			pod := &corev1.Pod{}
			require.NoError(t, runtime.DefaultUnstructuredConverter.FromUnstructured(obj.Object, pod))
			if selected {
				for _, container := range pod.Spec.Containers {
					require.Equal(t, new(true), container.SecurityContext.ReadOnlyRootFilesystem)
				}
				require.Equal(t, corev1.SeccompProfileTypeRuntimeDefault, pod.Spec.SecurityContext.SeccompProfile.Type)
				require.Equal(t, corev1.AppArmorProfileTypeRuntimeDefault, pod.Spec.SecurityContext.AppArmorProfile.Type)
			} else {
				require.Nil(t, pod.Spec.SecurityContext)
			}
			response = validation.PodRules(nil, nil, nil).OnCreate(nil, nil, pod, nil, events.NewEventRecorder(nil, logr.Discard(), nil, nil), tnt, bodies)(t.Context(), req)
			if selected && name == "override-denied" {
				require.NotNil(t, response)
				require.False(t, response.Allowed)
				require.Contains(t, response.Result.Message, "seccomp profile")
			} else {
				require.Nil(t, response)
			}
		}
	}
}
