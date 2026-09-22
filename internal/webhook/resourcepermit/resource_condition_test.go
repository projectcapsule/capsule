// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package resourcepermit

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/go-logr/logr"
	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/internal/cache"
	apiruntime "github.com/projectcapsule/capsule/pkg/api/runtime"
	"github.com/projectcapsule/capsule/pkg/runtime/handlers"
)

func TestPermitTemplateResourceConditions(t *testing.T) {
	conditions, err := cache.NewCELCache()
	if err != nil {
		t.Fatal(err)
	}
	for _, global := range []bool{false, true} {
		for _, operation := range []admissionv1.Operation{admissionv1.Create, admissionv1.Update} {
			for _, tc := range []struct {
				expression string
				valid      bool
			}{
				{"", true}, {"false", true}, {"true", true}, {"object == null", true},
				{"now > timestamp('2026-01-01T00:00:00Z')", true},
				{"42", false}, {"missingVariable", false}, {"object..invalid", false}, {" ", false},
			} {
				t.Run(fmt.Sprintf("global=%t/%s/%q", global, operation, tc.expression), func(t *testing.T) {
					run, req := resourceConditionAdmission(t, conditions, global, operation, tc.expression, 2)
					response := run(t.Context(), req)
					if tc.valid {
						if response != nil {
							t.Fatalf("valid condition rejected: %+v", response)
						}
						return
					}
					if response == nil || response.Allowed || response.Result == nil || !strings.Contains(response.Result.Message, "spec.resources[0].policy.condition") {
						t.Fatalf("invalid condition was not rejected at its field path: %+v", response)
					}
				})
			}
		}
	}
	if conditions.Stats() != 4 {
		t.Fatalf("expected four compiled conditions shared across template kinds and operations, got %d", conditions.Stats())
	}
}

func resourceConditionAdmission(tb testing.TB, conditions *cache.CELCache, global bool, operation admissionv1.Operation, expression string, blocks int) (handlers.Func, admission.Request) {
	tb.Helper()
	scheme := runtime.NewScheme()
	if err := capsulev1beta2.AddToScheme(scheme); err != nil {
		tb.Fatal(err)
	}
	resources := make([]apiruntime.ResourceTemplate, blocks)
	for i := range resources {
		resources[i] = apiruntime.ResourceTemplate{Policy: apiruntime.ResourceTemplatePolicy{Condition: expression}, Targets: []runtime.RawExtension{{Object: &corev1.ConfigMap{APIVersion: "v1", Kind: "ConfigMap", Name: fmt.Sprintf("target-%d", i)}}}}
	}
	var source runtime.Object = &capsulev1beta2.ResourcePermitTemplate{Spec: capsulev1beta2.ResourcePermitTemplateSpec{Resources: resources}}
	handler := ResourcePermitTemplateValidationHandler(logr.Discard(), conditions)
	if global {
		source = &capsulev1beta2.GlobalResourcePermitTemplate{Spec: capsulev1beta2.GlobalResourcePermitTemplateSpec{Resources: resources}}
		handler = GlobalResourcePermitTemplateValidationHandler(logr.Discard(), conditions)
	}
	data, err := json.Marshal(source)
	if err != nil {
		tb.Fatal(err)
	}
	decoder := admission.NewDecoder(scheme)
	run := handler.OnCreate(nil, nil, decoder, nil)
	if operation == admissionv1.Update {
		run = handler.OnUpdate(nil, nil, decoder, nil)
	}
	return run, admission.Request{AdmissionRequest: admissionv1.AdmissionRequest{Operation: operation, Object: runtime.RawExtension{Raw: data}}}
}

func BenchmarkPermitTemplateConditionAdmission(b *testing.B) {
	for _, global := range []bool{false, true} {
		for _, blocks := range []int{1, 32} {
			for _, expression := range []string{"", "object == null || now > timestamp(object.metadata.creationTimestamp) + duration('5m')"} {
				b.Run(fmt.Sprintf("global=%t/blocks=%d/condition=%t", global, blocks, expression != ""), func(b *testing.B) {
					conditions, err := cache.NewCELCache()
					if err != nil {
						b.Fatal(err)
					}
					run, req := resourceConditionAdmission(b, conditions, global, admissionv1.Update, expression, blocks)
					if response := run(b.Context(), req); response != nil {
						b.Fatalf("condition rejected: %+v", response)
					}
					b.ReportAllocs()
					for b.Loop() {
						if response := run(b.Context(), req); response != nil {
							b.Fatalf("condition rejected: %+v", response)
						}
					}
				})
			}
		}
	}
}
