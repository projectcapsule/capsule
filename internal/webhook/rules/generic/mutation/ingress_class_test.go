// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package mutation

import (
	"testing"

	"github.com/projectcapsule/capsule/internal/cache"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/api/rules"
	"github.com/projectcapsule/capsule/pkg/ruleengine"
	"github.com/projectcapsule/capsule/pkg/runtime/events"
	"github.com/stretchr/testify/require"
	admissionv1 "k8s.io/api/admission/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func ingressClassObject(kind rules.IngressType) *unstructured.Unstructured {
	group := "networking.k8s.io/v1"
	if kind == rules.IngressTypeGateway {
		group = "gateway.networking.k8s.io/v1"
	}
	return &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": group, "kind": string(kind),
		"metadata": map[string]any{"name": "app", "namespace": "selected", "labels": map[string]any{"retain": "yes"}},
		"spec":     map[string]any{"unrelated": map[string]any{"retain": "yes"}},
		"status":   map[string]any{"retain": "yes"},
	}}
}

func ingressClassMutation(kind rules.IngressType, action rules.MutationAction, name string) rules.NamespaceRuleMutation {
	return rules.NamespaceRuleMutation{Action: action, Ingress: rules.IngressMutation{Types: []rules.IngressType{kind}, Class: name}}
}

func TestMutateIngressClasses(t *testing.T) {
	for _, kind := range []rules.IngressType{rules.IngressTypeIngress, rules.IngressTypeGateway} {
		field := "ingressClassName"
		if kind == rules.IngressTypeGateway {
			field = "gatewayClassName"
		}
		for _, tc := range []struct {
			name, current, annotation string
			action                    rules.MutationAction
			changed                   bool
			want                      string
		}{
			{name: "default", changed: true, want: "edge"},
			{name: "explicit", current: "explicit", want: "explicit"},
			{name: "replace", current: "explicit", action: rules.MutationActionReplace, changed: true, want: "edge"},
			{name: "replace identical", current: "edge", action: rules.MutationActionReplace, want: "edge"},
		} {
			t.Run(string(kind)+"/"+tc.name, func(t *testing.T) {
				obj := ingressClassObject(kind)
				if tc.current != "" {
					require.NoError(t, unstructured.SetNestedField(obj.Object, tc.current, "spec", field))
				}
				before := obj.DeepCopy()
				body := &rules.NamespaceRuleBodyNamespace{Mutate: []rules.NamespaceRuleMutation{ingressClassMutation(kind, tc.action, "edge")}}
				originalRule := body.DeepCopy()
				changed, err := mutateIngressClass(t.Context(), obj, kind, []*rules.NamespaceRuleBodyNamespace{nil, body}, nil)
				require.NoError(t, err)
				require.Equal(t, tc.changed, changed)
				actual, _, err := unstructured.NestedString(obj.Object, "spec", field)
				require.NoError(t, err)
				require.Equal(t, tc.want, actual)
				unstructured.RemoveNestedField(before.Object, "spec", field)
				unstructured.RemoveNestedField(obj.Object, "spec", field)
				require.Equal(t, before, obj)
				require.Equal(t, originalRule, body)
			})
		}
	}
}

func TestIngressClassAnnotationAndAtomicity(t *testing.T) {
	for _, replace := range []bool{false, true} {
		obj := ingressClassObject(rules.IngressTypeIngress)
		obj.Object["metadata"].(map[string]any)["annotations"] = map[string]any{meta.IngressClassAnnotation: "legacy", "unrelated": "retained"}
		before := obj.DeepCopy()
		mutation := ingressClassMutation(rules.IngressTypeIngress, "", "edge")
		if replace {
			mutation.Action = rules.MutationActionReplace
		}
		changed, err := mutateIngressClass(t.Context(), obj, rules.IngressTypeIngress, []*rules.NamespaceRuleBodyNamespace{{Mutate: []rules.NamespaceRuleMutation{mutation}}}, nil)
		require.NoError(t, err)
		require.Equal(t, replace, changed)
		if replace {
			require.Equal(t, "edge", obj.GetAnnotations()[meta.IngressClassAnnotation])
			require.Equal(t, "retained", obj.GetAnnotations()["unrelated"])
			require.Equal(t, "legacy", before.GetAnnotations()[meta.IngressClassAnnotation], "copy shared maps before mutation")
		} else {
			require.Equal(t, before, obj)
		}
	}
	compiler, err := cache.NewCELCache()
	require.NoError(t, err)
	obj := ingressClassObject(rules.IngressTypeIngress)
	first := ingressClassMutation(rules.IngressTypeIngress, "", "first")
	second := ingressClassMutation(rules.IngressTypeIngress, rules.MutationActionReplace, "second")
	second.Conditions = []rules.AdmissionCondition{{Expression: `object.spec.ingressClassName == 'first'`}}
	bodies := []*rules.NamespaceRuleBodyNamespace{{Mutate: []rules.NamespaceRuleMutation{first, second, ingressClassMutation(rules.IngressTypeIngress, "", "last")}}}
	req := listenerSetRequest(t, obj, admissionv1.Create, "")
	conditions := ruleengine.NewConditionEvaluator(compiler, req.AdmissionRequest)
	changed, err := mutateIngressClass(t.Context(), obj, rules.IngressTypeIngress, bodies, conditions)
	require.NoError(t, err)
	require.True(t, changed)
	value, _, err := unstructured.NestedString(obj.Object, "spec", "ingressClassName")
	require.NoError(t, err)
	require.Equal(t, "second", value)
	before := obj.DeepCopy()
	second.Conditions[0].Expression = "object.missing == true"
	bodies[0].Mutate = []rules.NamespaceRuleMutation{ingressClassMutation(rules.IngressTypeIngress, rules.MutationActionReplace, "changed"), second}
	_, err = mutateIngressClass(t.Context(), obj, rules.IngressTypeIngress, bodies, conditions)
	require.ErrorContains(t, err, "conditions[0]")
	require.Equal(t, before, obj, "an error must not publish partial class changes")
}

func TestIngressClassMutationHandlerOperations(t *testing.T) {
	for _, operation := range []admissionv1.Operation{admissionv1.Create, admissionv1.Update} {
		for _, subresource := range []string{"", "status"} {
			obj := ingressClassObject(rules.IngressTypeGateway)
			req := listenerSetRequest(t, obj, operation, subresource)
			body := &rules.NamespaceRuleBodyNamespace{Mutate: []rules.NamespaceRuleMutation{ingressClassMutation(rules.IngressTypeGateway, "", "edge")}}
			h := MetadataRules(nil)
			handle := h.OnCreate(nil, nil, obj, nil, events.NewDiscardRecorder(), nil, []*rules.NamespaceRuleBodyNamespace{body})
			if operation == admissionv1.Update {
				handle = h.OnUpdate(nil, nil, obj.DeepCopy(), obj, nil, events.NewDiscardRecorder(), nil, []*rules.NamespaceRuleBodyNamespace{body})
			}
			response := handle(t.Context(), req)
			if subresource != "" {
				require.Nil(t, response)
			} else {
				require.NotNil(t, response)
				require.True(t, response.Allowed)
				require.NotEmpty(t, response.Patches)
			}
		}
	}
}
