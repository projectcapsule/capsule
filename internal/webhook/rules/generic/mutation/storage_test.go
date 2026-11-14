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
	authenticationv1 "k8s.io/api/authentication/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/internal/cache"
	pvcrules "github.com/projectcapsule/capsule/internal/webhook/rules/pvc/validation"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/api/rules"
	"github.com/projectcapsule/capsule/pkg/ruleengine"
	"github.com/projectcapsule/capsule/pkg/runtime/events"
)

// omitted marks a claim whose spec lacks the class field.
type omitted struct{}

func attributeClassDefault(action rules.MutationAction, class string) rules.NamespaceRuleMutation {
	return rules.NamespaceRuleMutation{Action: action, Storage: rules.StorageMutation{AttributeClass: class}}
}

func claimObject(class any) *unstructured.Unstructured {
	spec := map[string]any{"accessModes": []any{"ReadWriteOnce"}, "resources": map[string]any{"requests": map[string]any{"storage": "1Gi"}}}
	if _, skip := class.(omitted); !skip {
		spec[attributeClassField] = class
	}

	return &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "v1", "kind": "PersistentVolumeClaim",
		"metadata": map[string]any{"name": "data", "namespace": "selected", "labels": map[string]any{"tier": "fast"}},
		"spec":     spec, "status": map[string]any{"phase": "Pending"},
	}}
}

func TestAttributeClassDefaults(t *testing.T) {
	merge := func(class string) rules.NamespaceRuleMutation {
		return attributeClassDefault(rules.MutationActionMerge, class)
	}
	replace := func(class string) rules.NamespaceRuleMutation {
		return attributeClassDefault(rules.MutationActionReplace, class)
	}

	for _, tc := range []struct {
		name    string
		current any
		entries []rules.NamespaceRuleMutation
		want    any
		changed bool
	}{
		{name: "merge fills an omitted class", current: omitted{}, entries: []rules.NamespaceRuleMutation{merge("gold")}, want: "gold", changed: true},
		{name: "merge fills a null class", current: nil, entries: []rules.NamespaceRuleMutation{merge("gold")}, want: "gold", changed: true},
		{name: "an omitted action merges", current: omitted{}, entries: []rules.NamespaceRuleMutation{attributeClassDefault("", "gold")}, want: "gold", changed: true},
		{name: "merge keeps an empty class, which chooses none", current: "", entries: []rules.NamespaceRuleMutation{merge("gold")}, want: ""},
		{name: "merge keeps an explicit class", current: "silver", entries: []rules.NamespaceRuleMutation{merge("gold")}, want: "silver"},
		{name: "replace sets the class over an explicit one", current: "silver", entries: []rules.NamespaceRuleMutation{replace("gold")}, want: "gold", changed: true},
		{name: "replace sets the class over an empty one", current: "", entries: []rules.NamespaceRuleMutation{replace("gold")}, want: "gold", changed: true},
		{name: "replace with the same class changes nothing", current: "gold", entries: []rules.NamespaceRuleMutation{replace("gold")}, want: "gold"},
		{name: "the first merge wins", current: omitted{}, entries: []rules.NamespaceRuleMutation{merge("gold"), merge("silver")}, want: "gold", changed: true},
		{name: "a later replace overrides a merge", current: omitted{}, entries: []rules.NamespaceRuleMutation{merge("gold"), replace("silver")}, want: "silver", changed: true},
		{name: "a later merge keeps a replace", current: omitted{}, entries: []rules.NamespaceRuleMutation{replace("silver"), merge("gold")}, want: "silver", changed: true},
		{name: "the last replace wins", current: "bronze", entries: []rules.NamespaceRuleMutation{replace("gold"), replace("silver")}, want: "silver", changed: true},
		{name: "entries without a class are ignored", current: omitted{}, entries: []rules.NamespaceRuleMutation{{Devices: rules.DeviceMutation{Class: "gpu"}}}, want: omitted{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			obj := claimObject(tc.current)
			original := claimObject(tc.current)
			body := &rules.NamespaceRuleBodyNamespace{Mutate: tc.entries}
			before := body.DeepCopy()
			spec := obj.Object["spec"].(map[string]any)

			changed, err := mutateAttributeClassDefaults(t.Context(), obj, []*rules.NamespaceRuleBodyNamespace{nil, body}, nil)
			require.NoError(t, err)
			require.Equal(t, tc.changed, changed)

			value, found := obj.Object["spec"].(map[string]any)[attributeClassField]
			if _, skip := tc.want.(omitted); skip {
				require.False(t, found)
			} else {
				require.Equal(t, tc.want, value)
			}

			require.Equal(t, before, body, "rules owned by the cache must not change")
			require.Equal(t, original.Object["spec"], spec, "the decoded request must not change in place")

			for _, field := range []string{"metadata", "status"} {
				require.Equal(t, original.Object[field], obj.Object[field])
			}
		})
	}

	t.Run("a claim without a spec gets one", func(t *testing.T) {
		obj := claimObject(omitted{})
		delete(obj.Object, "spec")
		changed, err := mutateAttributeClassDefaults(t.Context(), obj, []*rules.NamespaceRuleBodyNamespace{{Mutate: []rules.NamespaceRuleMutation{merge("gold")}}}, nil)
		require.NoError(t, err)
		require.True(t, changed)
		require.Equal(t, map[string]any{attributeClassField: "gold"}, obj.Object["spec"])
	})

	t.Run("a claim is not inspected without class defaults", func(t *testing.T) {
		obj := claimObject(omitted{})
		obj.Object["spec"] = "invalid"
		changed, err := mutateAttributeClassDefaults(t.Context(), obj, []*rules.NamespaceRuleBodyNamespace{{Mutate: []rules.NamespaceRuleMutation{{Devices: rules.DeviceMutation{Class: "gpu"}}}}}, nil)
		require.NoError(t, err)
		require.False(t, changed)
	})

	for name, spec := range map[string]any{"spec": "invalid", "class": map[string]any{attributeClassField: int64(1)}} {
		t.Run("a malformed "+name+" is an error", func(t *testing.T) {
			obj := claimObject(omitted{})
			obj.Object["spec"] = spec
			before := obj.DeepCopy()
			changed, err := mutateAttributeClassDefaults(t.Context(), obj, []*rules.NamespaceRuleBodyNamespace{{Mutate: []rules.NamespaceRuleMutation{merge("gold")}}}, nil)
			require.Error(t, err)
			require.False(t, changed)
			require.Equal(t, before, obj)
		})
	}
}

func TestAttributeClassDefaultConditions(t *testing.T) {
	compiler, err := cache.NewCELCache()
	require.NoError(t, err)

	run := func(t *testing.T, current any, entries ...rules.NamespaceRuleMutation) (*unstructured.Unstructured, bool, error) {
		t.Helper()

		obj := claimObject(current)
		req := listenerSetRequest(t, obj, admissionv1.Create, "")
		changed, err := mutateAttributeClassDefaults(t.Context(), obj, []*rules.NamespaceRuleBodyNamespace{{Mutate: entries}}, ruleengine.NewConditionEvaluator(compiler, req.AdmissionRequest))

		return obj, changed, err
	}
	when := func(mutation rules.NamespaceRuleMutation, expression string) rules.NamespaceRuleMutation {
		mutation.Conditions = []rules.AdmissionCondition{{Name: "condition", Expression: expression}}

		return mutation
	}
	class := func(obj *unstructured.Unstructured) any {
		return obj.Object["spec"].(map[string]any)[attributeClassField]
	}

	t.Run("an unmatched entry is skipped", func(t *testing.T) {
		obj, changed, err := run(t, omitted{}, when(attributeClassDefault("", "gold"), "false"), attributeClassDefault("", "silver"))
		require.NoError(t, err)
		require.True(t, changed)
		require.Equal(t, "silver", class(obj))
	})

	t.Run("conditions see the claim", func(t *testing.T) {
		obj, changed, err := run(t, omitted{}, when(attributeClassDefault("", "gold"), `object.metadata.labels.tier == "fast"`))
		require.NoError(t, err)
		require.True(t, changed)
		require.Equal(t, "gold", class(obj))
	})

	t.Run("conditions see the class an earlier entry set", func(t *testing.T) {
		obj, changed, err := run(t, omitted{}, attributeClassDefault("", "gold"),
			when(attributeClassDefault(rules.MutationActionReplace, "silver"), `object.spec.volumeAttributesClassName == "gold"`))
		require.NoError(t, err)
		require.True(t, changed)
		require.Equal(t, "silver", class(obj))
	})

	t.Run("a later condition sees the class an earlier conditional entry set", func(t *testing.T) {
		obj, changed, err := run(t, omitted{}, when(attributeClassDefault("", "gold"), `object.metadata.labels.tier == "fast"`),
			when(attributeClassDefault(rules.MutationActionReplace, "silver"), `object.spec.volumeAttributesClassName == "gold"`))
		require.NoError(t, err)
		require.True(t, changed)
		require.Equal(t, "silver", class(obj))
	})

	t.Run("a merge that can't apply doesn't evaluate its conditions", func(t *testing.T) {
		obj, changed, err := run(t, "bronze", when(attributeClassDefault("", "gold"), `object.missing == true`))
		require.NoError(t, err)
		require.False(t, changed)
		require.Equal(t, "bronze", class(obj))
	})

	for name, entry := range map[string]rules.NamespaceRuleMutation{
		"a failed condition":    when(attributeClassDefault(rules.MutationActionReplace, "silver"), `object.missing == true`),
		"an unsupported action": attributeClassDefault("unknown", "silver"),
	} {
		t.Run(name+" leaves the claim unchanged", func(t *testing.T) {
			obj, changed, err := run(t, omitted{}, attributeClassDefault("", "gold"), entry)
			require.Error(t, err)
			require.False(t, changed)
			require.Equal(t, claimObject(omitted{}), obj)
		})
	}
}

func TestAttributeClassDefaultHandlerGates(t *testing.T) {
	h := &metadataRules{}
	bodies := []*rules.NamespaceRuleBodyNamespace{{Mutate: []rules.NamespaceRuleMutation{attributeClassDefault("", "gold")}}}

	t.Run("claim creation", func(t *testing.T) {
		obj := claimObject(omitted{})
		req := listenerSetRequest(t, obj, admissionv1.Create, "")
		response := h.OnCreate(nil, nil, obj, nil, nil, nil, bodies)(t.Context(), req)
		require.NotNil(t, response)
		require.True(t, response.Allowed)
		require.Len(t, response.Patches, 1)
		require.Equal(t, "/spec/volumeAttributesClassName", response.Patches[0].Path)
		require.Equal(t, "gold", response.Patches[0].Value)
	})

	t.Run("updates never change the class", func(t *testing.T) {
		obj, old := claimObject(omitted{}), claimObject(omitted{})
		req := listenerSetRequest(t, obj, admissionv1.Update, "")
		require.Nil(t, h.OnUpdate(nil, nil, old, obj, nil, nil, nil, bodies)(t.Context(), req))
	})

	t.Run("other kinds", func(t *testing.T) {
		obj := claimObject(omitted{})
		obj.SetKind("PersistentVolume")
		req := listenerSetRequest(t, obj, admissionv1.Create, "")
		require.Nil(t, h.OnCreate(nil, nil, obj, nil, nil, nil, bodies)(t.Context(), req))
	})

	t.Run("subresources", func(t *testing.T) {
		obj := claimObject(omitted{})
		req := listenerSetRequest(t, obj, admissionv1.Update, "status")
		require.Nil(t, h.OnUpdate(nil, nil, obj, obj, nil, nil, nil, bodies)(t.Context(), req))
	})
}

// The validating webhooks see the class a default set, so attribute class
// rules check it like a class the claim names.
func TestAttributeClassDefaultRegistrationAndEnforcement(t *testing.T) {
	for _, fallback := range []bool{false, true} {
		t.Run(fmt.Sprintf("fallback=%t", fallback), func(t *testing.T) {
			c, reads, decoder := mutationRegistrationFixture(t, 100)
			body := &rules.NamespaceRuleBodyNamespace{
				Mutate:  []rules.NamespaceRuleMutation{attributeClassDefault("", "gold")},
				Enforce: &rules.NamespaceRuleEnforceBody{Action: rules.ActionTypeAllow, Storage: rules.NamespaceRuleEnforceStorageBody{AttributeClasses: []rules.StorageClassMatch{{Exact: []string{"gold"}}}}},
			}
			ruleset(t, c, body, fallback)

			for _, current := range []any{omitted{}, "forbidden"} {
				obj := claimObject(current)
				req := listenerSetRequest(t, obj, admissionv1.Create, "")
				req.UserInfo = authenticationv1.UserInfo{Username: "alice"}
				reads.Store(0)

				response := Register(nil, nil).GetHandlers()[0].OnCreate(c, c, decoder, events.NewDiscardRecorder())(t.Context(), req)
				require.EqualValues(t, 3, reads.Load(), "defaulting must not read the VolumeAttributesClass")

				if current == "forbidden" {
					require.Nil(t, response)
				} else {
					require.NotNil(t, response)
					require.True(t, response.Allowed)
					req.Object.Raw = applyPatches(t, req.Object.Raw, response)
				}

				decision := pvcrules.AttributeClassHandler(nil, nil, nil, nil).OnCreate(c, c, decoder, events.NewDiscardRecorder())(t.Context(), req)
				if current == "forbidden" {
					require.NotNil(t, decision)
					require.False(t, decision.Allowed)
					require.Contains(t, decision.Result.Message, `volume attributes class "forbidden" at spec.volumeAttributesClassName is not allowed by namespace rule`)
				} else {
					require.Nil(t, decision, "the class the default set is allowed")
				}
			}
		})
	}

	t.Run("a default for another audience", func(t *testing.T) {
		c, _, decoder := mutationRegistrationFixture(t, 1)
		body := &rules.NamespaceRuleBodyNamespace{Audience: []rules.Audience{{Kind: rules.AudienceKindUser, Name: "bob"}}, Mutate: []rules.NamespaceRuleMutation{attributeClassDefault("", "gold")}}
		ruleset(t, c, body, false)

		req := listenerSetRequest(t, claimObject(omitted{}), admissionv1.Create, "")
		req.UserInfo = authenticationv1.UserInfo{Username: "alice"}
		require.Nil(t, Register(nil, nil).GetHandlers()[0].OnCreate(c, c, decoder, events.NewDiscardRecorder())(t.Context(), req))

		req.UserInfo = authenticationv1.UserInfo{Username: "bob"}
		response := Register(nil, nil).GetHandlers()[0].OnCreate(c, c, decoder, events.NewDiscardRecorder())(t.Context(), req)
		require.NotNil(t, response)
		require.Len(t, response.Patches, 1)
	})
}

// ruleset stores the rules of the fixture's namespace, in the RuleStatus or,
// as a fallback, in the Tenant.
func ruleset(t *testing.T, c client.Client, body *rules.NamespaceRuleBodyNamespace, fallback bool) {
	t.Helper()

	status := &capsulev1beta2.RuleStatus{}
	require.NoError(t, c.Get(t.Context(), client.ObjectKey{Namespace: "selected", Name: meta.NameForManagedRuleStatus()}, status))

	if fallback {
		require.NoError(t, c.Delete(t.Context(), status))

		tnt := &capsulev1beta2.Tenant{}
		require.NoError(t, c.Get(t.Context(), client.ObjectKey{Name: "tenant"}, tnt))
		tnt.Spec.Rules = []*rules.NamespaceRuleBodyTenant{{NamespaceRuleBodyNamespace: body}}
		require.NoError(t, c.Update(t.Context(), tnt))

		return
	}

	status.Spec = []*rules.NamespaceRuleBodyNamespace{body}
	status.Status.Rules = []*rules.NamespaceRuleBodyNamespace{body}
	require.NoError(t, c.Update(t.Context(), status))
}

// applyPatches applies a mutation response, as the API server does before the
// validating webhooks run.
func applyPatches(t *testing.T, raw []byte, response *admission.Response) []byte {
	t.Helper()

	encoded, err := json.Marshal(response.Patches)
	require.NoError(t, err)

	patch, err := jsonpatch.DecodePatch(encoded)
	require.NoError(t, err)

	patched, err := patch.Apply(raw)
	require.NoError(t, err)

	return patched
}
