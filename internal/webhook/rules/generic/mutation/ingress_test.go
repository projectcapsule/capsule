// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package mutation

import (
	"encoding/json"
	"fmt"
	"testing"

	jsonpatch "github.com/evanphx/json-patch/v5"
	"github.com/stretchr/testify/require"
	admissionv1 "k8s.io/api/admission/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/internal/cache"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/api/rules"
	"github.com/projectcapsule/capsule/pkg/ruleengine"
	"github.com/projectcapsule/capsule/pkg/runtime/events"
)

func listenerSetObject() *unstructured.Unstructured {
	return &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "gateway.networking.k8s.io/v1", "kind": "ListenerSet",
		"metadata": map[string]any{"name": "app", "namespace": "selected"},
		"spec":     map[string]any{"listeners": []any{map[string]any{"name": "http", "port": int64(80), "protocol": "HTTP", "hostname": "app.example.com"}}},
		"status":   map[string]any{"unrelated": "retained"},
	}}
}

func TestListenerSetMutationRegistrationReadsAndFallback(t *testing.T) {
	for _, fallback := range []bool{false, true} {
		t.Run(fmt.Sprintf("fallback=%t", fallback), func(t *testing.T) {
			c, reads, decoder := mutationRegistrationFixture(t, 16)
			body := &rules.NamespaceRuleBodyNamespace{Mutate: []rules.NamespaceRuleMutation{listenerSetMutation("edge", "")}}
			if fallback {
				status := &capsulev1beta2.RuleStatus{}
				require.NoError(t, c.Get(t.Context(), client.ObjectKey{Namespace: "selected", Name: meta.NameForManagedRuleStatus()}, status))
				require.NoError(t, c.Delete(t.Context(), status))
				tnt := &capsulev1beta2.Tenant{}
				require.NoError(t, c.Get(t.Context(), client.ObjectKey{Name: "tenant"}, tnt))
				tnt.Spec.Rules = []*rules.NamespaceRuleBodyTenant{{NamespaceRuleBodyNamespace: body}}
				require.NoError(t, c.Update(t.Context(), tnt))
			} else {
				status := &capsulev1beta2.RuleStatus{}
				require.NoError(t, c.Get(t.Context(), client.ObjectKey{Namespace: "selected", Name: meta.NameForManagedRuleStatus()}, status))
				status.Status.Rules = []*rules.NamespaceRuleBodyNamespace{body}
				require.NoError(t, c.Update(t.Context(), status))
			}
			reads.Store(0)
			h := Register(nil, nil).GetHandlers()[0]
			req := listenerSetRequest(t, listenerSetObject(), admissionv1.Create, "")
			response := h.OnCreate(c, c, decoder, events.NewDiscardRecorder())(t.Context(), req)
			require.NotNil(t, response)
			require.True(t, response.Allowed)
			require.NotEmpty(t, response.Patches)
			require.EqualValues(t, 3, reads.Load(), "reference defaulting must not read the Gateway")
			reads.Store(0)
			req.SubResource, req.Operation = "status", admissionv1.Update
			req.Object.Raw = []byte("invalid JSON")
			require.Nil(t, h.OnUpdate(c, c, decoder, events.NewDiscardRecorder())(t.Context(), req))
			require.Zero(t, reads.Load())
		})
	}
}

func listenerSetMutation(name string, action rules.MutationAction) rules.NamespaceRuleMutation {
	return rules.NamespaceRuleMutation{Action: action, Ingress: rules.IngressMutation{ListenerSets: rules.ListenerSetMutation{ParentRef: &rules.GatewayParentReference{Name: name, Namespace: new("platform")}}}}
}

func listenerSetRequest(tb testing.TB, obj *unstructured.Unstructured, operation admissionv1.Operation, subresource string) admission.Request {
	tb.Helper()
	raw, err := json.Marshal(obj)
	require.NoError(tb, err)
	req := admission.Request{}
	req.Kind.Group, req.Kind.Version, req.Kind.Kind = obj.GroupVersionKind().Group, obj.GroupVersionKind().Version, obj.GroupVersionKind().Kind
	req.Operation, req.SubResource, req.Namespace, req.Name = operation, subresource, obj.GetNamespace(), obj.GetName()
	req.Object, req.OldObject = runtime.RawExtension{Raw: raw}, runtime.RawExtension{Raw: raw}
	return req
}

func TestListenerSetParentReferenceMutation(t *testing.T) {
	for _, tc := range []struct {
		name      string
		current   any
		mutations []rules.NamespaceRuleMutation
		want      any
		changed   bool
	}{
		{"merge omitted", nil, []rules.NamespaceRuleMutation{listenerSetMutation("edge", "")}, map[string]any{"name": "edge", "namespace": "platform", "group": "gateway.networking.k8s.io", "kind": "Gateway"}, true},
		{"merge explicit", map[string]any{"name": "custom"}, []rules.NamespaceRuleMutation{listenerSetMutation("edge", rules.MutationActionMerge)}, map[string]any{"name": "custom"}, false},
		{"merge malformed", map[string]any{}, []rules.NamespaceRuleMutation{listenerSetMutation("edge", rules.MutationActionMerge)}, map[string]any{}, false},
		{"merge scalar", "invalid", []rules.NamespaceRuleMutation{listenerSetMutation("edge", rules.MutationActionMerge)}, "invalid", false},
		{"replace atomic", map[string]any{"name": "custom", "namespace": "old", "extension": "removed"}, []rules.NamespaceRuleMutation{listenerSetMutation("edge", rules.MutationActionReplace)}, map[string]any{"name": "edge", "namespace": "platform", "group": "gateway.networking.k8s.io", "kind": "Gateway"}, true},
		{"first merge wins", nil, []rules.NamespaceRuleMutation{listenerSetMutation("first", ""), listenerSetMutation("second", "")}, map[string]any{"name": "first", "namespace": "platform", "group": "gateway.networking.k8s.io", "kind": "Gateway"}, true},
		{"later replacement wins", nil, []rules.NamespaceRuleMutation{listenerSetMutation("first", ""), listenerSetMutation("second", rules.MutationActionReplace)}, map[string]any{"name": "second", "namespace": "platform", "group": "gateway.networking.k8s.io", "kind": "Gateway"}, true},
		{"identical replacement", map[string]any{"name": "edge", "namespace": "platform", "group": "gateway.networking.k8s.io", "kind": "Gateway"}, []rules.NamespaceRuleMutation{listenerSetMutation("edge", rules.MutationActionReplace)}, map[string]any{"name": "edge", "namespace": "platform", "group": "gateway.networking.k8s.io", "kind": "Gateway"}, false},
		{"cancelled replacements", map[string]any{"name": "edge", "namespace": "platform", "group": "gateway.networking.k8s.io", "kind": "Gateway"}, []rules.NamespaceRuleMutation{listenerSetMutation("temporary", rules.MutationActionReplace), listenerSetMutation("edge", rules.MutationActionReplace)}, map[string]any{"name": "edge", "namespace": "platform", "group": "gateway.networking.k8s.io", "kind": "Gateway"}, false},
		{"no mutation", nil, nil, nil, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			obj := listenerSetObject()
			if tc.current != nil {
				obj.Object["spec"].(map[string]any)["parentRef"] = tc.current
			}
			before := obj.DeepCopy()
			bodies := []*rules.NamespaceRuleBodyNamespace{nil, {Mutate: tc.mutations}}
			originalRules := bodies[1].DeepCopy()
			changed, err := mutateListenerSetParentRef(t.Context(), obj, bodies, nil)
			require.NoError(t, err)
			require.Equal(t, tc.changed, changed)
			ref, _, err := unstructured.NestedFieldNoCopy(obj.Object, "spec", "parentRef")
			require.NoError(t, err)
			require.Equal(t, tc.want, ref)
			unstructured.RemoveNestedField(obj.Object, "spec", "parentRef")
			unstructured.RemoveNestedField(before.Object, "spec", "parentRef")
			require.Equal(t, before, obj)
			require.Equal(t, originalRules, bodies[1])
		})
	}
}

func TestListenerSetReferenceDefaultsAndIsolation(t *testing.T) {
	mutation := listenerSetMutation("edge", rules.MutationActionReplace)
	mutation.Ingress.ListenerSets.ParentRef.Namespace = nil
	mutation.Ingress.ListenerSets.ParentRef.Group = new("")
	mutation.Ingress.ListenerSets.ParentRef.Kind = new("Service")
	body := &rules.NamespaceRuleBodyNamespace{Mutate: []rules.NamespaceRuleMutation{mutation}}
	obj := listenerSetObject()
	require.NoError(t, unstructured.SetNestedMap(obj.Object, map[string]any{"name": "old", "namespace": "old"}, "spec", "parentRef"))
	changed, err := mutateListenerSetParentRef(t.Context(), obj, []*rules.NamespaceRuleBodyNamespace{body}, nil)
	require.NoError(t, err)
	require.True(t, changed)
	ref, _, _ := unstructured.NestedMap(obj.Object, "spec", "parentRef")
	require.Equal(t, map[string]any{"name": "edge", "group": "", "kind": "Service"}, ref)
	obj.Object["spec"].(map[string]any)["parentRef"].(map[string]any)["name"] = "changed"
	require.Equal(t, "edge", body.Mutate[0].Ingress.ListenerSets.ParentRef.Name)
	body.Mutate[0].Action = rules.MutationActionMerge
	obj.Object["spec"].(map[string]any)["parentRef"] = nil
	changed, err = mutateListenerSetParentRef(t.Context(), obj, []*rules.NamespaceRuleBodyNamespace{body}, nil)
	require.NoError(t, err)
	require.True(t, changed)
	changed, err = mutateListenerSetParentRef(t.Context(), nil, []*rules.NamespaceRuleBodyNamespace{body}, nil)
	require.NoError(t, err)
	require.False(t, changed)
}

func TestListenerSetMutationConditionsAndErrors(t *testing.T) {
	compiler, err := cache.NewCELCache()
	require.NoError(t, err)
	first, second := listenerSetMutation("first", ""), listenerSetMutation("second", rules.MutationActionReplace)
	second.Conditions = []rules.AdmissionCondition{{Expression: `object.spec.parentRef.name == 'first' && request.operation == 'CREATE'`}}
	obj := listenerSetObject()
	req := listenerSetRequest(t, obj, admissionv1.Create, "")
	body := &rules.NamespaceRuleBodyNamespace{Mutate: []rules.NamespaceRuleMutation{first, second}}
	changed, err := mutateListenerSetParentRef(t.Context(), obj, []*rules.NamespaceRuleBodyNamespace{body}, ruleengine.NewConditionEvaluator(compiler, req.AdmissionRequest))
	require.NoError(t, err)
	require.True(t, changed)
	name, _, _ := unstructured.NestedString(obj.Object, "spec", "parentRef", "name")
	require.Equal(t, "second", name)
	for _, expression := range []string{"false", "object.missing == true"} {
		obj = listenerSetObject()
		before := obj.DeepCopy()
		body.Mutate[1].Conditions[0].Expression = expression
		changed, err = mutateListenerSetParentRef(t.Context(), obj, []*rules.NamespaceRuleBodyNamespace{body}, ruleengine.NewConditionEvaluator(compiler, req.AdmissionRequest))
		if expression == "false" {
			require.NoError(t, err)
			require.True(t, changed)
		} else {
			require.ErrorContains(t, err, "rules[0].mutate[1]")
			require.False(t, changed)
			require.Equal(t, before, obj)
		}
	}
	obj = listenerSetObject()
	obj.Object["spec"] = "malformed"
	_, err = mutateListenerSetParentRef(t.Context(), obj, []*rules.NamespaceRuleBodyNamespace{{Mutate: []rules.NamespaceRuleMutation{first}}}, nil)
	require.ErrorContains(t, err, "spec.parentRef")
	first.Action = "invalid"
	_, err = mutateListenerSetParentRef(t.Context(), listenerSetObject(), []*rules.NamespaceRuleBodyNamespace{{Mutate: []rules.NamespaceRuleMutation{first}}}, nil)
	require.ErrorContains(t, err, "rules[0].mutate[0].action")
}

func TestListenerSetMutationHandlerOperations(t *testing.T) {
	compiler, err := cache.NewCELCache()
	require.NoError(t, err)
	for _, tc := range []struct {
		name, group, version, kind, subresource string
		operation                               admissionv1.Operation
		mutate                                  bool
	}{
		{"create", listenerSetKind.Group, "v1", "ListenerSet", "", admissionv1.Create, true},
		{"update", listenerSetKind.Group, "v1", "ListenerSet", "", admissionv1.Update, true},
		{"dry run", listenerSetKind.Group, "v1", "ListenerSet", "", admissionv1.Create, true},
		{"status", listenerSetKind.Group, "v1", "ListenerSet", "status", admissionv1.Update, false},
		{"delete", listenerSetKind.Group, "v1", "ListenerSet", "", admissionv1.Delete, false},
		{"other group", "example.com", "v1", "ListenerSet", "", admissionv1.Create, false},
		{"old version", listenerSetKind.Group, "v1beta1", "ListenerSet", "", admissionv1.Create, false},
		{"other kind", listenerSetKind.Group, "v1", "Gateway", "", admissionv1.Create, false},
		{"pod", "", "v1", "Pod", "", admissionv1.Create, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			obj := listenerSetObject()
			obj.SetAPIVersion(tc.version)
			if tc.group != "" {
				obj.SetAPIVersion(tc.group + "/" + tc.version)
			}
			obj.SetKind(tc.kind)
			if tc.kind == "Pod" {
				obj.Object["spec"] = map[string]any{"containers": []any{map[string]any{"name": "app", "image": "nginx"}}}
			}
			mutation := listenerSetMutation("edge", "")
			mutation.Conditions = []rules.AdmissionCondition{{Expression: `object.spec.listeners[0].name == 'http'`}}
			req := listenerSetRequest(t, obj, tc.operation, tc.subresource)
			if tc.name == "dry run" {
				req.DryRun = new(true)
			}
			h := MetadataRules(compiler)
			fn := h.OnCreate(nil, nil, obj, nil, nil, nil, []*rules.NamespaceRuleBodyNamespace{{Mutate: []rules.NamespaceRuleMutation{mutation}}})
			if tc.operation == admissionv1.Update {
				fn = h.OnUpdate(nil, nil, obj.DeepCopy(), obj, nil, nil, nil, []*rules.NamespaceRuleBodyNamespace{{Mutate: []rules.NamespaceRuleMutation{mutation}}})
			}
			if tc.operation == admissionv1.Delete {
				fn = h.OnDelete(nil, nil, obj, nil, nil, nil, nil)
			}
			response := fn(t.Context(), req)
			if tc.mutate {
				require.NotNil(t, response)
				require.True(t, response.Allowed)
				require.NotEmpty(t, response.Patches)
			} else {
				require.Nil(t, response)
			}
		})
	}
}

func TestListenerSetMetadataAndReferenceSharePatch(t *testing.T) {
	compiler, err := cache.NewCELCache()
	require.NoError(t, err)
	for _, gated := range []bool{false, true} {
		t.Run(fmt.Sprintf("metadata-gated=%t", gated), func(t *testing.T) {
			obj := listenerSetObject()
			req := listenerSetRequest(t, obj, admissionv1.Create, "")
			body := &rules.NamespaceRuleBodyNamespace{
				Mutate: []rules.NamespaceRuleMutation{listenerSetMutation("edge", "")},
				Enforce: &rules.NamespaceRuleEnforceBody{Action: rules.ActionTypeAllow, Metadata: []rules.MetadataRule{{
					APIGroups: []string{"gateway.networking.k8s.io/v1"}, Kinds: []string{"ListenerSet"}, Labels: map[string]rules.MetadataValueRule{"profile-default": {Default: new("applied")}},
				}}},
			}
			if gated {
				body.Enforce.Conditions = []rules.AdmissionCondition{{Expression: "false"}}
			}
			before := body.DeepCopy()
			response := MetadataRules(compiler).OnCreate(nil, nil, obj, nil, nil, nil, []*rules.NamespaceRuleBodyNamespace{body})(t.Context(), req)
			require.NotNil(t, response)
			require.True(t, response.Allowed)
			ref, _, err := unstructured.NestedString(obj.Object, "spec", "parentRef", "name")
			require.NoError(t, err)
			require.Equal(t, "edge", ref)
			if gated {
				require.NotContains(t, obj.GetLabels(), "profile-default")
			} else {
				require.Equal(t, "applied", obj.GetLabels()["profile-default"])
			}
			encoded, err := json.Marshal(response.Patches)
			require.NoError(t, err)
			patch, err := jsonpatch.DecodePatch(encoded)
			require.NoError(t, err)
			patched, err := patch.Apply(req.Object.Raw)
			require.NoError(t, err)
			want, err := json.Marshal(obj)
			require.NoError(t, err)
			require.JSONEq(t, string(want), string(patched))
			require.Equal(t, before, body)
		})
	}
}
