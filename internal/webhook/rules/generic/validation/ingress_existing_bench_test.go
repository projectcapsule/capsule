// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package validation

import (
	"fmt"
	"testing"

	admissionv1 "k8s.io/api/admission/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/rules"
	apiruntime "github.com/projectcapsule/capsule/pkg/api/runtime"
	"github.com/projectcapsule/capsule/pkg/runtime/events"
)

func BenchmarkExistingIngressAdmission(b *testing.B) {
	for _, count := range []int{1, 32} {
		for _, state := range []string{"skip", "hostname"} {
			b.Run(fmt.Sprintf("rules=%d/%s", count, state), func(b *testing.B) {
				obj := objectWithSpec(map[string]any{"rules": []any{map[string]any{"host": "app.example.com"}}})
				bodies := make([]*rules.NamespaceRuleBodyNamespace, count)
				for i := range bodies {
					enforce := &rules.NamespaceRuleEnforceBody{}
					if state == "hostname" {
						enforce.Action = rules.ActionTypeAllow
						enforce.Ingress = rules.NamespaceRuleEnforceIngressBody{Types: []rules.IngressType{rules.IngressTypeIngress}, Hostnames: []apiruntime.ExpressionMatch{{Exact: []string{"app.example.com"}}}}
					}
					bodies[i] = &rules.NamespaceRuleBodyNamespace{Enforce: enforce}
				}
				req := admission.Request{Operation: admissionv1.Create, Kind: metav1.GroupVersionKind{Group: "networking.k8s.io", Version: "v1", Kind: "Ingress"}}
				h := testIngressRules()
				recorder := events.NewDiscardRecorder()
				tenant := &capsulev1beta2.Tenant{Name: "tenant"}
				b.ReportAllocs()
				for b.Loop() {
					if response := h.OnCreate(nil, nil, obj, nil, recorder, tenant, bodies)(b.Context(), req); response != nil {
						b.Fatalf("unexpected hostname/skip response: %+v", response)
					}
				}
			})
		}
	}
}
