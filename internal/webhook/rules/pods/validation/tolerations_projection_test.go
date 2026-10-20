// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package validation

import (
	"testing"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/require"
	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
	"sigs.k8s.io/yaml"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/rules"
	"github.com/projectcapsule/capsule/pkg/runtime/events"
	"github.com/projectcapsule/capsule/pkg/tenant"
)

func TestTolerationOnlyRuleProjectionAndAdmission(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))
	require.NoError(t, capsulev1beta2.AddToScheme(scheme))

	for _, tc := range []struct {
		name, match string
		blocked     []string
	}{
		{name: "all tolerations", match: "- {}", blocked: []string{"border", "node.kubernetes.io/not-ready"}},
		{name: "listed keys", match: "- key: {exact: [border, node-role.kubernetes.io/master, node-role.kubernetes.io/control-plane], negate: false}", blocked: []string{"border", "node-role.kubernetes.io/master", "node-role.kubernetes.io/control-plane"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var tnt capsulev1beta2.Tenant
			require.NoError(t, yaml.UnmarshalStrict([]byte(`
apiVersion: capsule.clastix.io/v1beta2
kind: Tenant
metadata:
  name: toleration-tenant
spec:
  rules:
    - namespaceSelector:
        matchLabels:
          profile: restricted
      enforce:
        action: deny
        workloads:
          placement:
            tolerations:
              `+tc.match+`
`), &tnt))
			original := tnt.DeepCopy()
			namespace := &corev1.Namespace{Name: "restricted", Labels: map[string]string{"profile": "restricted"}}
			bodies, err := tenant.BuildNamespaceRuleBodyStatus(scheme, namespace, &tnt)
			require.NoError(t, err)
			require.Len(t, bodies, 1)
			require.Equal(t, tnt.Spec.Rules[0].Enforce.Workloads.Placement.Tolerations, bodies[0].Enforce.Workloads.Placement.Tolerations)
			require.Equal(t, original, &tnt)

			handler := PodRules(nil, nil, nil)
			recorder := events.NewEventRecorder(nil, logr.Discard(), nil, nil)
			request := admission.Request{Operation: admissionv1.Create}
			for _, key := range tc.blocked {
				request.Operation = admissionv1.Create
				pod := &corev1.Pod{Name: "blocked", Namespace: namespace.Name, Spec: corev1.PodSpec{
					Tolerations: []corev1.Toleration{{Key: key, Operator: corev1.TolerationOpEqual, Value: "true", Effect: corev1.TaintEffectNoExecute}},
				}}
				response := handler.OnCreate(nil, nil, pod, nil, recorder, &tnt, bodies)(t.Context(), request)
				require.NotNil(t, response, "key %q must be denied", key)
				require.False(t, response.Allowed)
				require.Contains(t, response.Result.Message, "spec.tolerations[0]")
				require.Contains(t, response.Result.Message, key)

				request.Operation = admissionv1.Update
				response = handler.OnUpdate(nil, nil, &corev1.Pod{}, pod, nil, recorder, &tnt, bodies)(t.Context(), request)
				require.NotNil(t, response)
				require.False(t, response.Allowed)
				require.Contains(t, response.Result.Message, "spec.tolerations[0]")
			}

			request.Operation = admissionv1.Create
			require.Nil(t, handler.OnCreate(nil, nil, &corev1.Pod{}, nil, recorder, &tnt, bodies)(t.Context(), request))
			namespace.Labels["profile"] = "unrestricted"
			unselected, err := tenant.BuildNamespaceRuleBodyStatus(scheme, namespace, &tnt)
			require.NoError(t, err)
			require.Empty(t, unselected)
			pod := &corev1.Pod{Spec: corev1.PodSpec{Tolerations: []corev1.Toleration{{Key: "border", Operator: corev1.TolerationOpEqual, Value: "true", Effect: corev1.TaintEffectNoExecute}}}}
			require.Nil(t, handler.OnCreate(nil, nil, pod, nil, recorder, &tnt, unselected)(t.Context(), request))
		})
	}
}

func TestFlatTolerationRulesAreNotSupported(t *testing.T) {
	for _, source := range []string{
		`enforce: {action: deny, workloads: {tolerations: [{}]}}`,
		`enforce: {action: deny, workloads: {tolerations: [{key: {exact: [border, node-role.kubernetes.io/master, node-role.kubernetes.io/control-plane], negate: false}}]}}`,
	} {
		var body rules.NamespaceRuleBodyNamespace
		require.ErrorContains(t, yaml.UnmarshalStrict([]byte(source), &body), `unknown field "tolerations"`)
	}
}
