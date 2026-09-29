// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package validation

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/go-logr/logr"
	admissionv1 "k8s.io/api/admission/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/internal/cache"
	"github.com/projectcapsule/capsule/pkg/api/rules"
	apiruntime "github.com/projectcapsule/capsule/pkg/api/runtime"
	"github.com/projectcapsule/capsule/pkg/runtime/events"
)

func TestEnforcementConditionsGateMetadataAndIngress(t *testing.T) {
	compiler, err := cache.NewCELCache()
	if err != nil {
		t.Fatal(err)
	}
	recorder := events.NewEventRecorder(nil, logr.Discard(), nil, nil)
	tenant := &capsulev1beta2.Tenant{ObjectMeta: metav1.ObjectMeta{Name: "tenant-a"}}
	for _, kind := range []string{"ConfigMap", "Namespace", "Ingress"} {
		for _, tc := range []struct{ name, expression, failure string }{
			{"false skips", "false", ""},
			{"true denies", "true", "blocked"},
			{"full resource available", "object.data.enabled == 'yes'", "blocked"},
			{"runtime error fails closed", "object.data.missing == 'yes'", "enforce: enforcement rule[0]: conditions[0]"},
			{"false dominates error", "false", ""},
		} {
			t.Run(kind+"/"+tc.name, func(t *testing.T) {
				group := ""
				if kind == "Ingress" {
					group = "networking.k8s.io"
				}
				obj := &unstructured.Unstructured{Object: map[string]any{
					"metadata": map[string]any{"name": "example", "namespace": "tenant-a", "labels": map[string]any{"blocked": "yes"}},
					"data":     map[string]any{"enabled": "yes"},
					"spec":     map[string]any{"rules": []any{map[string]any{"host": "blocked.example.com"}}},
				}}
				raw, err := json.Marshal(obj)
				if err != nil {
					t.Fatal(err)
				}
				req := admission.Request{AdmissionRequest: admissionv1.AdmissionRequest{Kind: metav1.GroupVersionKind{Group: group, Version: "v1", Kind: kind}, Object: runtime.RawExtension{Raw: raw}}}
				bodies := []*rules.NamespaceRuleBodyNamespace{{Enforce: &rules.NamespaceRuleEnforceBody{
					Action:     rules.ActionTypeDeny,
					Conditions: []rules.AdmissionCondition{{Name: "gate", Expression: tc.expression}},
					Metadata:   []rules.MetadataRule{{VersionKinds: apiruntime.VersionKinds{APIGroups: []string{"v1"}, Kinds: []string{kind}}, Labels: map[string]rules.MetadataValueRule{"blocked": {}}}},
					Ingress:    rules.NamespaceRuleEnforceIngressBody{Types: []rules.IngressType{rules.IngressTypeIngress}, Hostnames: []apiruntime.ExpressionMatch{{Exact: []string{"blocked.example.com"}}}},
				}}}
				if tc.name == "false dominates error" {
					bodies[0].Enforce.Conditions = append([]rules.AdmissionCondition{{Expression: "object.missing == 'x'"}}, bodies[0].Enforce.Conditions...)
				}
				var response *admission.Response
				if kind == "Ingress" {
					response = IngressRules(nil, compiler).OnCreate(nil, nil, obj, nil, recorder, tenant, bodies)(t.Context(), req)
				} else {
					metadata := &metav1.PartialObjectMetadata{ObjectMeta: metav1.ObjectMeta{Name: "example", Namespace: "tenant-a", Labels: obj.GetLabels()}}
					response = GenericRules(nil, compiler).OnCreate(nil, nil, metadata, nil, recorder, tenant, bodies)(t.Context(), req)
				}
				if tc.failure == "" {
					if response != nil {
						t.Fatalf("skipped gate must continue handler chain: %#v", response)
					}
				} else if response == nil || response.Allowed || !strings.Contains(response.Result.Message, tc.failure) {
					t.Fatalf("expected %q denial, got %#v", tc.failure, response)
				}
			})
		}
	}
}

func TestMetadataSkipsConditionsForUnrelatedKind(t *testing.T) {
	compiler, err := cache.NewCELCache()
	if err != nil {
		t.Fatal(err)
	}
	bodies := []*rules.NamespaceRuleBodyNamespace{{Enforce: &rules.NamespaceRuleEnforceBody{
		Conditions: []rules.AdmissionCondition{{Expression: "object.spec.containers.size() > 0"}},
		Metadata:   []rules.MetadataRule{{VersionKinds: apiruntime.VersionKinds{APIGroups: []string{"v1"}, Kinds: []string{"Pod"}}, Labels: map[string]rules.MetadataValueRule{"required": {Required: true}}}},
	}}}
	req := admission.Request{AdmissionRequest: admissionv1.AdmissionRequest{Kind: metav1.GroupVersionKind{Version: "v1", Kind: "ConfigMap"}}}
	response := GenericRules(nil, compiler).OnCreate(nil, nil, &metav1.PartialObjectMetadata{}, nil, nil, nil, bodies)(t.Context(), req)
	if response != nil || compiler.Stats() != 0 {
		t.Fatalf("unrelated condition was evaluated: response=%#v cache=%d", response, compiler.Stats())
	}
}

func BenchmarkConditionalMetadataValidation(b *testing.B) {
	compiler, err := cache.NewCELCache()
	if err != nil {
		b.Fatal(err)
	}
	for _, count := range []int{1, 20} {
		for _, mode := range []string{"ungated", "allow", "skip", "error"} {
			for _, size := range []int{128, 64 << 10} {
				b.Run(fmt.Sprintf("rules=%d/bytes=%d/%s", count, size, mode), func(b *testing.B) {
					bodies := make([]*rules.NamespaceRuleBodyNamespace, count)
					for i := range bodies {
						bodies[i] = &rules.NamespaceRuleBodyNamespace{Enforce: &rules.NamespaceRuleEnforceBody{Action: rules.ActionTypeAllow, Metadata: []rules.MetadataRule{{VersionKinds: apiruntime.VersionKinds{APIGroups: []string{"v1"}, Kinds: []string{"ConfigMap"}}, Labels: map[string]rules.MetadataValueRule{"profile": {Required: true}}}}}}
						expression := "object.data.enabled == 'yes'"
						if mode == "skip" {
							expression = "object.data.enabled == 'no'"
						}
						if mode == "error" {
							expression = "object.data.missing == 'x'"
						}
						if mode != "ungated" {
							bodies[i].Enforce.Conditions = []rules.AdmissionCondition{{Expression: expression}}
						}
					}
					obj := &metav1.PartialObjectMetadata{ObjectMeta: metav1.ObjectMeta{Name: "test", Labels: map[string]string{"profile": "shared"}}}
					req := admission.Request{AdmissionRequest: admissionv1.AdmissionRequest{Kind: metav1.GroupVersionKind{Version: "v1", Kind: "ConfigMap"}, Object: runtime.RawExtension{Raw: []byte(`{"metadata":{"labels":{"profile":"shared"}},"data":{"enabled":"yes"}}`)}}}
					raw, err := json.Marshal(map[string]any{"metadata": map[string]any{"labels": map[string]string{"profile": "shared"}}, "data": map[string]string{"enabled": "yes", "payload": strings.Repeat("x", size)}})
					if err != nil {
						b.Fatal(err)
					}
					req.Object.Raw = raw
					handler := GenericRules(nil, compiler)
					handler.OnCreate(nil, nil, obj, nil, nil, nil, bodies)(b.Context(), req)
					b.ReportAllocs()
					b.ResetTimer()
					for b.Loop() {
						response := handler.OnCreate(nil, nil, obj, nil, nil, nil, bodies)(b.Context(), req)
						if mode == "error" {
							if response == nil || response.Allowed {
								b.Fatal("expected rejection")
							}
						} else if response != nil {
							b.Fatal("expected continued handler chain")
						}
					}
				})
			}
		}
	}
}
