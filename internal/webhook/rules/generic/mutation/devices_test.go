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
	"sigs.k8s.io/controller-runtime/pkg/client"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/internal/cache"
	"github.com/projectcapsule/capsule/internal/webhook/dra"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/api/rules"
	"github.com/projectcapsule/capsule/pkg/ruleengine"
	"github.com/projectcapsule/capsule/pkg/runtime/events"
)

func deviceDefault(name string) rules.NamespaceRuleMutation {
	return rules.NamespaceRuleMutation{Devices: rules.DeviceMutation{Class: name}}
}

func deviceDefaultObject(version, kind string, requests []any) *unstructured.Unstructured {
	spec := map[string]any{"devices": map[string]any{"requests": requests}, "unrelated": "retained"}
	if kind == "ResourceClaimTemplate" {
		spec = map[string]any{"metadata": map[string]any{"labels": map[string]any{"template": "retained"}}, "spec": spec}
	}
	return &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "resource.k8s.io/" + version, "kind": kind,
		"metadata": map[string]any{"name": "claim", "namespace": "selected", "labels": map[string]any{"unrelated": "retained"}},
		"spec":     spec, "status": map[string]any{"unrelated": "retained"},
	}}
}

func deviceDefaultRequests(obj *unstructured.Unstructured) []any {
	path := []string{"spec", "devices", "requests"}
	if obj.GetKind() == "ResourceClaimTemplate" {
		path = append([]string{"spec"}, path...)
	}
	value, _, _ := unstructured.NestedSlice(obj.Object, path...)
	return value
}

func TestDeviceClassDefaults(t *testing.T) {
	for _, version := range []string{"v1", "v1beta2"} {
		for _, kind := range []string{"ResourceClaim", "ResourceClaimTemplate"} {
			for _, tc := range []struct {
				name    string
				fields  map[string]any
				want    map[string]any
				changed bool
			}{
				{"omitted", map[string]any{}, map[string]any{"deviceClassName": "gpu"}, true},
				{"null", map[string]any{"deviceClassName": nil}, map[string]any{"deviceClassName": "gpu"}, true},
				{"empty", map[string]any{"deviceClassName": ""}, map[string]any{"deviceClassName": "gpu"}, true},
				{"explicit", map[string]any{"deviceClassName": "custom"}, map[string]any{"deviceClassName": "custom"}, false},
				{"preserve settings", map[string]any{"count": int64(2), "adminAccess": true, "selectors": []any{map[string]any{"cel": map[string]any{"expression": "true"}}}}, map[string]any{"deviceClassName": "gpu", "count": int64(2), "adminAccess": true, "selectors": []any{map[string]any{"cel": map[string]any{"expression": "true"}}}}, true},
			} {
				t.Run(version+"/"+kind+"/"+tc.name, func(t *testing.T) {
					obj := deviceDefaultObject(version, kind, []any{map[string]any{"name": "request", "exactly": tc.fields}})
					original := obj.DeepCopy()
					body := &rules.NamespaceRuleBodyNamespace{Mutate: []rules.NamespaceRuleMutation{deviceDefault("gpu"), deviceDefault("later")}}
					before := body.DeepCopy()
					changed, err := mutateDeviceClassDefaults(t.Context(), obj, []*rules.NamespaceRuleBodyNamespace{nil, body}, nil)
					require.NoError(t, err)
					require.Equal(t, tc.changed, changed)
					require.Equal(t, tc.want, deviceDefaultRequests(obj)[0].(map[string]any)["exactly"])
					require.Equal(t, before, body)
					// No borrowed input map is modified, including on successful mutation.
					require.Equal(t, original.Object["spec"], deviceDefaultObject(version, kind, []any{map[string]any{"name": "request", "exactly": tc.fields}}).Object["spec"])
					for _, field := range []string{"metadata", "status"} {
						require.Equal(t, original.Object[field], obj.Object[field])
					}
					changed, err = mutateDeviceClassDefaults(t.Context(), obj, []*rules.NamespaceRuleBodyNamespace{body}, nil)
					require.NoError(t, err)
					require.False(t, changed, "reinvocation must retain the selected default")
				})
			}
			t.Run(version+"/"+kind+"/mixed alternatives", func(t *testing.T) {
				requests := []any{
					map[string]any{"name": "exact", "exactly": map[string]any{}},
					map[string]any{"name": "fallbacks", "firstAvailable": []any{map[string]any{"name": "one", "deviceClassName": "custom", "count": int64(2)}, map[string]any{"name": "two"}, map[string]any{"name": "three", "deviceClassName": ""}}},
				}
				obj := deviceDefaultObject(version, kind, requests)
				before := obj.DeepCopy()
				changed, err := mutateDeviceClassDefaults(t.Context(), obj, []*rules.NamespaceRuleBodyNamespace{{Mutate: []rules.NamespaceRuleMutation{deviceDefault("gpu")}}}, nil)
				require.NoError(t, err)
				require.True(t, changed)
				got := deviceDefaultRequests(obj)
				require.Equal(t, "gpu", got[0].(map[string]any)["exactly"].(map[string]any)["deviceClassName"])
				alternatives := got[1].(map[string]any)["firstAvailable"].([]any)
				require.Equal(t, map[string]any{"name": "one", "deviceClassName": "custom", "count": int64(2)}, alternatives[0])
				for _, alt := range alternatives[1:] {
					require.Equal(t, "gpu", alt.(map[string]any)["deviceClassName"])
				}
				require.Equal(t, deviceDefaultRequests(before), requests, "the original request projection must remain immutable")
			})
		}
	}
	changed, err := mutateDeviceClassDefaults(t.Context(), nil, nil, nil)
	require.NoError(t, err)
	require.False(t, changed)
}

func TestDeviceDefaultConditionsAndAtomicErrors(t *testing.T) {
	compiler, err := cache.NewCELCache()
	require.NoError(t, err)
	obj := deviceDefaultObject("v1", "ResourceClaim", []any{map[string]any{"name": "gpu", "exactly": map[string]any{}}})
	req := listenerSetRequest(t, obj, admissionv1.Create, "")
	first, second := deviceDefault("first"), deviceDefault("second")
	first.Conditions = []rules.AdmissionCondition{{Expression: "false"}}
	body := &rules.NamespaceRuleBodyNamespace{Mutate: []rules.NamespaceRuleMutation{first, second}}
	changed, err := mutateDeviceClassDefaults(t.Context(), obj, []*rules.NamespaceRuleBodyNamespace{body}, ruleengine.NewConditionEvaluator(compiler, req.AdmissionRequest))
	require.NoError(t, err)
	require.True(t, changed)
	require.Equal(t, "second", deviceDefaultRequests(obj)[0].(map[string]any)["exactly"].(map[string]any)["deviceClassName"])
	obj = deviceDefaultObject("v1", "ResourceClaim", []any{map[string]any{"name": "gpu", "exactly": map[string]any{}}})
	first.Conditions = nil
	second.Conditions = []rules.AdmissionCondition{{Expression: `object.spec.devices.requests[0].exactly.deviceClassName == 'first' || object.missing == true`}}
	body.Mutate = []rules.NamespaceRuleMutation{first, second}
	changed, err = mutateDeviceClassDefaults(t.Context(), obj, []*rules.NamespaceRuleBodyNamespace{body}, ruleengine.NewConditionEvaluator(compiler, req.AdmissionRequest))
	require.NoError(t, err)
	require.True(t, changed)
	for _, action := range []rules.MutationAction{rules.MutationActionMerge, rules.MutationActionReplace, "unknown"} {
		obj = deviceDefaultObject("v1", "ResourceClaim", []any{map[string]any{"name": "gpu", "exactly": map[string]any{}}})
		before := obj.DeepCopy()
		second = deviceDefault("second")
		second.Action = action
		if action == rules.MutationActionMerge {
			second.Conditions = []rules.AdmissionCondition{{Expression: "object.missing == true"}}
		}
		body.Mutate = []rules.NamespaceRuleMutation{first, second}
		changed, err = mutateDeviceClassDefaults(t.Context(), obj, []*rules.NamespaceRuleBodyNamespace{body}, ruleengine.NewConditionEvaluator(compiler, req.AdmissionRequest))
		require.Error(t, err)
		require.False(t, changed)
		require.Equal(t, before, obj)
	}
}

func TestDeviceDefaultMalformedAndAbsentRequests(t *testing.T) {
	body := &rules.NamespaceRuleBodyNamespace{Mutate: []rules.NamespaceRuleMutation{deviceDefault("gpu")}}
	for _, raw := range []string{`{}`, `{"requests":null}`, `{"requests":[]}`, `{"requests":[{"name":"missing-form"}]}`, `{"requests":[{"name":"null-exact","exactly":null}]}`} {
		obj := deviceDefaultObject("v1", "ResourceClaim", nil)
		var devices any
		require.NoError(t, json.Unmarshal([]byte(raw), &devices))
		obj.Object["spec"].(map[string]any)["devices"] = devices
		before := obj.DeepCopy()
		changed, err := mutateDeviceClassDefaults(t.Context(), obj, []*rules.NamespaceRuleBodyNamespace{body}, nil)
		require.NoError(t, err)
		require.False(t, changed)
		require.Equal(t, before, obj)
	}
	for _, bad := range []any{int64(1), map[string]any{"exactly": "invalid"}, map[string]any{"exactly": map[string]any{"deviceClassName": true}}, map[string]any{"firstAvailable": "invalid"}, map[string]any{"firstAvailable": []any{nil}}, map[string]any{"firstAvailable": []any{map[string]any{"deviceClassName": int64(1)}}}} {
		obj := deviceDefaultObject("v1", "ResourceClaim", []any{map[string]any{"exactly": map[string]any{}}, bad})
		before := obj.DeepCopy()
		changed, err := mutateDeviceClassDefaults(t.Context(), obj, []*rules.NamespaceRuleBodyNamespace{body}, nil)
		require.Error(t, err)
		require.False(t, changed)
		require.Equal(t, before, obj)
	}
	for _, spec := range []any{"invalid", map[string]any{"devices": true}, map[string]any{"devices": map[string]any{"requests": map[string]any{}}}} {
		obj := deviceDefaultObject("v1", "ResourceClaim", nil)
		obj.Object["spec"] = spec
		before := obj.DeepCopy()
		changed, err := mutateDeviceClassDefaults(t.Context(), obj, []*rules.NamespaceRuleBodyNamespace{body}, nil)
		require.Error(t, err)
		require.False(t, changed)
		require.Equal(t, before, obj)
	}
}

func TestDeviceDefaultHandlerGatesAndMetadata(t *testing.T) {
	compiler, err := cache.NewCELCache()
	require.NoError(t, err)
	body := &rules.NamespaceRuleBodyNamespace{Mutate: []rules.NamespaceRuleMutation{deviceDefault("gpu")}, Enforce: &rules.NamespaceRuleEnforceBody{Metadata: []rules.MetadataRule{{APIGroups: []string{"resource.k8s.io"}, Kinds: []string{"ResourceClaim"}, Labels: map[string]rules.MetadataValueRule{"ready": {Default: new("yes")}}}}}}
	body.Mutate[0].Conditions = []rules.AdmissionCondition{{Expression: `object.metadata.labels.ready == 'yes'`}}
	obj := deviceDefaultObject("v1", "ResourceClaim", []any{map[string]any{"exactly": map[string]any{}}})
	req := listenerSetRequest(t, obj, admissionv1.Create, "")
	h := MetadataRules(compiler)
	response := h.OnCreate(nil, nil, obj, nil, nil, nil, []*rules.NamespaceRuleBodyNamespace{body})(t.Context(), req)
	require.NotNil(t, response)
	require.True(t, response.Allowed)
	require.NotEmpty(t, response.Patches)
	require.Equal(t, "yes", obj.GetLabels()["ready"])
	require.Equal(t, "gpu", deviceDefaultRequests(obj)[0].(map[string]any)["exactly"].(map[string]any)["deviceClassName"])
	for _, operation := range []admissionv1.Operation{admissionv1.Update, admissionv1.Delete} {
		obj = deviceDefaultObject("v1", "ResourceClaim", []any{map[string]any{"exactly": map[string]any{}}})
		req = listenerSetRequest(t, obj, operation, "")
		plain := &rules.NamespaceRuleBodyNamespace{Mutate: []rules.NamespaceRuleMutation{deviceDefault("gpu")}}
		require.Nil(t, h.OnUpdate(nil, nil, obj.DeepCopy(), obj, nil, nil, nil, []*rules.NamespaceRuleBodyNamespace{plain})(t.Context(), req))
	}
	for _, version := range []string{"v1", "v1beta2"} {
		for _, kind := range []string{"ResourceClaim", "ResourceClaimTemplate"} {
			obj = deviceDefaultObject(version, kind, []any{map[string]any{"exactly": map[string]any{"deviceClassName": "selected-before-admission"}}})
			req = listenerSetRequest(t, obj, admissionv1.Create, "")
			req.UserInfo.Username = "system:kube-scheduler"
			require.Nil(t, h.OnCreate(nil, nil, obj, nil, nil, nil, []*rules.NamespaceRuleBodyNamespace{{Mutate: []rules.NamespaceRuleMutation{deviceDefault("gpu")}}})(t.Context(), req), "scheduler-selected classes must never be replaced")
			req.Operation, req.SubResource = admissionv1.Update, "status"
			req.Object.Raw = []byte("invalid JSON")
			require.Nil(t, Register(nil, compiler).GetHandlers()[0].OnUpdate(nil, nil, nil, nil)(t.Context(), req))
		}
	}
	for _, kind := range []string{"DeviceClass", "Pod"} {
		obj = deviceDefaultObject("v1", kind, nil)
		req = listenerSetRequest(t, obj, admissionv1.Create, "")
		require.Nil(t, h.OnCreate(nil, nil, obj, nil, nil, nil, []*rules.NamespaceRuleBodyNamespace{{Mutate: []rules.NamespaceRuleMutation{deviceDefault("gpu")}}})(t.Context(), req))
	}
}

func TestDeviceDefaultRegistrationAndEnforcement(t *testing.T) {
	for _, fallback := range []bool{false, true} {
		t.Run(fmt.Sprintf("fallback=%t", fallback), func(t *testing.T) {
			c, reads, decoder := mutationRegistrationFixture(t, 100)
			body := &rules.NamespaceRuleBodyNamespace{Mutate: []rules.NamespaceRuleMutation{deviceDefault("gpu")}, Enforce: &rules.NamespaceRuleEnforceBody{Action: rules.ActionTypeAllow, Devices: rules.NamespaceRuleEnforceDevicesBody{Classes: []rules.DeviceClassMatch{{Exact: []string{"gpu"}}}}}}
			status := &capsulev1beta2.RuleStatus{}
			require.NoError(t, c.Get(t.Context(), client.ObjectKey{Namespace: "selected", Name: meta.NameForManagedRuleStatus()}, status))
			if fallback {
				require.NoError(t, c.Delete(t.Context(), status))
				tnt := &capsulev1beta2.Tenant{}
				require.NoError(t, c.Get(t.Context(), client.ObjectKey{Name: "tenant"}, tnt))
				tnt.Spec.Rules = []*rules.NamespaceRuleBodyTenant{{NamespaceRuleBodyNamespace: body}}
				require.NoError(t, c.Update(t.Context(), tnt))
			} else {
				status.Spec = []*rules.NamespaceRuleBodyNamespace{body}
				status.Status.Rules = []*rules.NamespaceRuleBodyNamespace{body}
				require.NoError(t, c.Update(t.Context(), status))
			}
			for _, explicit := range []string{"", "forbidden"} {
				obj := deviceDefaultObject("v1", "ResourceClaim", []any{map[string]any{"name": "gpu", "exactly": map[string]any{"deviceClassName": explicit}}})
				req := listenerSetRequest(t, obj, admissionv1.Create, "")
				req.DryRun = new(true)
				reads.Store(0)
				response := Register(nil, nil).GetHandlers()[0].OnCreate(c, c, decoder, events.NewDiscardRecorder())(t.Context(), req)
				require.EqualValues(t, profileReads(fallback), reads.Load(), "defaulting must not read the DeviceClass")
				if explicit == "" {
					require.NotNil(t, response)
					require.True(t, response.Allowed)
					encoded, err := json.Marshal(response.Patches)
					require.NoError(t, err)
					patch, err := jsonpatch.DecodePatch(encoded)
					require.NoError(t, err)
					req.Object.Raw, err = patch.Apply(req.Object.Raw)
					require.NoError(t, err)
				} else {
					require.Nil(t, response)
				}
				decision := dra.DeviceClass(nil, nil, nil, nil).OnCreate(c, c, decoder, events.NewDiscardRecorder())(t.Context(), req)
				if explicit == "" {
					require.Nil(t, decision)
				} else {
					require.NotNil(t, decision)
					require.False(t, decision.Allowed)
					require.Contains(t, decision.Result.Message, "not allowed")
				}
			}
		})
	}
}
