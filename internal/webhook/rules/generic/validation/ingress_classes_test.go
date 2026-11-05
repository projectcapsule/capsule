// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package validation

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	admissionv1 "k8s.io/api/admission/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	k8sruntime "k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/internal/cache"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/api/rules"
	"github.com/projectcapsule/capsule/pkg/runtime/events"
)

type ingressClassReader struct {
	client.Reader
	gets int
	err  error
}

func (r *ingressClassReader) Get(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
	r.gets++
	if r.err != nil {
		return r.err
	}
	return r.Reader.Get(ctx, key, obj, opts...)
}

func ingressClassRule(action rules.ActionType, kind rules.IngressType, matches ...rules.IngressClassMatch) *rules.NamespaceRuleEnforceBody {
	return &rules.NamespaceRuleEnforceBody{Action: action, Ingress: rules.NamespaceRuleEnforceIngressBody{Types: []rules.IngressType{kind}, Classes: matches}}
}

func ingressClassFixture(tb testing.TB, kind rules.IngressType, name string) (*unstructured.Unstructured, *ingressClassReader, *ingressRules) {
	tb.Helper()
	scheme := k8sruntime.NewScheme()
	require.NoError(tb, networkingv1.AddToScheme(scheme))
	require.NoError(tb, gatewayv1.Install(scheme))
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(
		&networkingv1.IngressClass{Name: "edge", Labels: map[string]string{"env": "customer"}},
		&networkingv1.IngressClass{Name: "other", Labels: map[string]string{"env": "external"}},
		&gatewayv1.GatewayClass{Name: "edge", Labels: map[string]string{"env": "customer"}},
	).Build()
	group, field := "networking.k8s.io", "ingressClassName"
	if kind == rules.IngressTypeGateway {
		group, field = "gateway.networking.k8s.io", "gatewayClassName"
	}
	obj := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": group + "/v1", "kind": string(kind),
		"metadata": map[string]any{"name": "app", "namespace": "tenant-a"}, "spec": map[string]any{},
	}}
	if name != "" {
		require.NoError(tb, unstructured.SetNestedField(obj.Object, name, "spec", field))
	}
	h := IngressRules(cache.NewRegexCache(), cache.NewLabelSelectorCache()).(*ingressRules)
	return obj, &ingressClassReader{Reader: c}, h
}

func TestIngressClassEnforcement(t *testing.T) {
	for _, kind := range []rules.IngressType{rules.IngressTypeIngress, rules.IngressTypeGateway} {
		for _, tc := range []struct {
			name, class   string
			action        rules.ActionType
			matches       []rules.IngressClassMatch
			blocked       bool
			failure       string
			reads, audits int
		}{
			{name: "exact", class: "edge", action: rules.ActionTypeAllow, matches: []rules.IngressClassMatch{{Exact: []string{"edge"}}}},
			{name: "allow miss", class: "other", action: rules.ActionTypeAllow, matches: []rules.IngressClassMatch{{Exact: []string{"edge"}}}, blocked: true},
			{name: "deny", class: "edge", action: rules.ActionTypeDeny, matches: []rules.IngressClassMatch{{Exact: []string{"edge"}}}, blocked: true},
			{name: "expression", class: "edge", action: rules.ActionTypeAllow, matches: []rules.IngressClassMatch{{Expression: "^ed.*"}}},
			{name: "selector", class: "edge", action: rules.ActionTypeAllow, matches: []rules.IngressClassMatch{{MatchLabels: map[string]string{"env": "customer"}}}, reads: 1},
			{name: "entries OR", class: "edge", action: rules.ActionTypeAllow, matches: []rules.IngressClassMatch{{MatchLabels: map[string]string{"env": "other"}}, {Exact: []string{"edge"}}}},
			{name: "criteria OR", class: "edge", action: rules.ActionTypeAllow, matches: []rules.IngressClassMatch{{Exact: []string{"edge"}, MatchLabels: map[string]string{"env": "other"}}}},
			{name: "deny selector", class: "edge", action: rules.ActionTypeDeny, matches: []rules.IngressClassMatch{{MatchLabels: map[string]string{"env": "customer"}}}, reads: 1, blocked: true},
			{name: "missing reference", class: "", action: rules.ActionTypeAllow, matches: []rules.IngressClassMatch{{Exact: []string{"edge"}}}, blocked: true},
			{name: "audit missing reference", class: "", action: rules.ActionTypeAudit, matches: []rules.IngressClassMatch{{Exact: []string{"edge"}}}, audits: 1},
			{name: "audit match", class: "edge", action: rules.ActionTypeAudit, matches: []rules.IngressClassMatch{{Exact: []string{"edge"}}}, audits: 1},
			{name: "unknown selector class", class: "missing", action: rules.ActionTypeDeny, matches: []rules.IngressClassMatch{{MatchLabels: map[string]string{"env": "customer"}}}, reads: 1, failure: "cannot be resolved"},
			{name: "audit unknown selector class", class: "missing", action: rules.ActionTypeAudit, matches: []rules.IngressClassMatch{{MatchLabels: map[string]string{"env": "customer"}}}, reads: 1},
		} {
			t.Run(string(kind)+"/"+tc.name, func(t *testing.T) {
				obj, reader, h := ingressClassFixture(t, kind, tc.class)
				body := ingressClassRule(tc.action, kind, tc.matches...)
				before := body.DeepCopy()
				result, err := h.evaluateClasses(t.Context(), reader, obj, kind, []*rules.NamespaceRuleEnforceBody{body})
				require.Equal(t, tc.reads, reader.gets)
				if tc.failure != "" {
					require.ErrorContains(t, err, tc.failure)
				} else {
					require.NoError(t, err)
					require.NotNil(t, result)
					require.Equal(t, tc.blocked, result.Blocking != nil)
					require.Len(t, result.Audits, tc.audits)
				}
				require.Equal(t, before, body)
			})
		}
	}
}

func TestIngressClassRepresentationsOrderingAndFreshness(t *testing.T) {
	obj, reader, h := ingressClassFixture(t, rules.IngressTypeIngress, "edge")
	allow := ingressClassRule(rules.ActionTypeAllow, rules.IngressTypeIngress, rules.IngressClassMatch{Exact: []string{"edge"}})
	obj.SetAnnotations(map[string]string{meta.IngressClassAnnotation: "other"})
	result, err := h.evaluateClasses(t.Context(), reader, obj, rules.IngressTypeIngress, []*rules.NamespaceRuleEnforceBody{allow})
	require.NoError(t, err)
	require.NotNil(t, result.Blocking, "must check the annotation even when spec selects an allowed class")
	unstructured.RemoveNestedField(obj.Object, "spec", "ingressClassName")
	obj.SetAnnotations(map[string]string{meta.IngressClassAnnotation: "edge"})
	result, err = h.evaluateClasses(t.Context(), reader, obj, rules.IngressTypeIngress, []*rules.NamespaceRuleEnforceBody{allow})
	require.NoError(t, err)
	require.Nil(t, result.Blocking)
	deny := ingressClassRule(rules.ActionTypeDeny, rules.IngressTypeIngress, rules.IngressClassMatch{Exact: []string{"edge"}})
	result, err = h.evaluateClasses(t.Context(), reader, obj, rules.IngressTypeIngress, []*rules.NamespaceRuleEnforceBody{deny, allow})
	require.NoError(t, err)
	require.Nil(t, result.Blocking, "last matching rule must win")
	result, err = h.evaluateClasses(t.Context(), reader, obj, rules.IngressTypeIngress, []*rules.NamespaceRuleEnforceBody{allow, deny})
	require.NoError(t, err)
	require.NotNil(t, result.Blocking)
	selector := ingressClassRule(rules.ActionTypeAllow, rules.IngressTypeIngress, rules.IngressClassMatch{MatchLabels: map[string]string{"env": "customer"}})
	reader.gets = 0
	result, err = h.evaluateClasses(t.Context(), reader, obj, rules.IngressTypeIngress, []*rules.NamespaceRuleEnforceBody{selector, selector, selector})
	require.NoError(t, err)
	require.Nil(t, result.Blocking)
	require.Equal(t, 1, reader.gets, "reuse the class read across all entries and rules")
	class := &networkingv1.IngressClass{}
	require.NoError(t, reader.Reader.Get(t.Context(), client.ObjectKey{Name: "edge"}, class))
	class.Labels["env"] = "external"
	require.NoError(t, reader.Reader.(client.Client).Update(t.Context(), class))
	result, err = h.evaluateClasses(t.Context(), reader, obj, rules.IngressTypeIngress, []*rules.NamespaceRuleEnforceBody{selector})
	require.NoError(t, err)
	require.NotNil(t, result.Blocking, "do not retain class label results across requests")
	reader.err = errors.New("class API unavailable")
	_, err = h.evaluateClasses(t.Context(), reader, obj, rules.IngressTypeIngress, []*rules.NamespaceRuleEnforceBody{selector})
	require.ErrorContains(t, err, "class API unavailable")
	_, err = h.evaluateClasses(t.Context(), reader, obj, rules.IngressTypeIngress, []*rules.NamespaceRuleEnforceBody{allow})
	require.NoError(t, err, "name matching must not depend on class reads")
}

type ingressClassEvents struct {
	events.EventRecorder
	calls int
}

func (r *ingressClassEvents) LabeledEvent(obj k8sruntime.Object, eventType, reason, action, note string) events.LabeledEvent {
	r.calls++
	return r.EventRecorder.LabeledEvent(obj, eventType, reason, action, note)
}

func TestIngressClassHandlerConditionsDryRunAndSkip(t *testing.T) {
	for _, tc := range []struct {
		name, condition, subresource string
		dry                          bool
		denied                       bool
		events                       int
	}{
		{name: "deny", denied: true, events: 1},
		{name: "condition false", condition: "false"},
		{name: "condition error", condition: "object.missing == true", denied: true},
		{name: "status", subresource: "status"},
		{name: "dry run", dry: true, denied: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			obj, reader, h := ingressClassFixture(t, rules.IngressTypeIngress, "edge")
			compiler, err := cache.NewCELCache()
			require.NoError(t, err)
			h.compiler = compiler
			body := ingressClassRule(rules.ActionTypeDeny, rules.IngressTypeIngress, rules.IngressClassMatch{Exact: []string{"edge"}})
			if tc.condition != "" {
				body.Conditions = []rules.AdmissionCondition{{Expression: tc.condition}}
			}
			raw, err := json.Marshal(obj)
			require.NoError(t, err)
			req := admission.Request{Operation: admissionv1.Create, Kind: metav1.GroupVersionKind{Group: "networking.k8s.io", Version: "v1", Kind: "Ingress"}, SubResource: tc.subresource, DryRun: new(tc.dry), Object: k8sruntime.RawExtension{Raw: raw}}
			recorder := &ingressClassEvents{EventRecorder: events.NewDiscardRecorder()}
			response := h.OnCreate(nil, reader, obj, nil, recorder, &capsulev1beta2.Tenant{Name: "tenant-a"}, []*rules.NamespaceRuleBodyNamespace{{Enforce: body}})(t.Context(), req)
			if tc.denied {
				require.NotNil(t, response)
				require.False(t, response.Allowed)
			} else {
				require.Nil(t, response)
			}
			require.Equal(t, tc.events, recorder.calls)
			require.Zero(t, reader.gets)
			require.Nil(t, h.OnDelete(nil, reader, obj, nil, recorder, nil, nil)(t.Context(), req))
		})
	}
}
