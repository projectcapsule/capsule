// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package mutation

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	admissionv1 "k8s.io/api/admission/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/projectcapsule/capsule/internal/cache"
	"github.com/projectcapsule/capsule/pkg/api/rules"
)

func BenchmarkIngressClassMutation(b *testing.B) {
	for _, kind := range []rules.IngressType{rules.IngressTypeIngress, rules.IngressTypeGateway} {
		for _, count := range []int{1, 32} {
			for _, tenants := range []int{1, 16} {
				for _, state := range []string{"default", "replace", "preserve", "condition-false", "status"} {
					b.Run(fmt.Sprintf("%s/rules=%d/tenants=%d/%s", kind, count, tenants, state), func(b *testing.B) {
						compiler, err := cache.NewCELCache()
						require.NoError(b, err)
						obj := ingressClassObject(kind)
						field := "ingressClassName"
						if kind == rules.IngressTypeGateway {
							field = "gatewayClassName"
						}
						if state == "replace" || state == "preserve" {
							require.NoError(b, unstructured.SetNestedField(obj.Object, "explicit", "spec", field))
						}
						req := listenerSetRequest(b, obj, admissionv1.Create, "")
						if state == "status" {
							req.Operation, req.SubResource = admissionv1.Update, "status"
						}
						profiles := make([][]*rules.NamespaceRuleBodyNamespace, tenants)
						names := make([]string, tenants)
						for tenant := range tenants {
							names[tenant] = fmt.Sprintf("tenant-%d-edge", tenant)
							for range count {
								m := ingressClassMutation(kind, "", names[tenant])
								if state == "replace" {
									m.Action = rules.MutationActionReplace
								}
								if state == "condition-false" {
									m.Conditions = []rules.AdmissionCondition{{Expression: "false"}}
								}
								profiles[tenant] = append(profiles[tenant], &rules.NamespaceRuleBodyNamespace{Mutate: []rules.NamespaceRuleMutation{m}})
							}
						}
						b.ReportAllocs()
						h := MetadataRules(compiler)
						for i := 0; b.Loop(); i++ {
							current := obj.DeepCopy()
							response := h.OnCreate(nil, nil, current, nil, nil, nil, profiles[i%tenants])(b.Context(), req)
							if state == "default" || state == "replace" {
								if response == nil || !response.Allowed || len(response.Patches) == 0 {
									b.Fatal("expected class mutation")
								}
								value, _, err := unstructured.NestedString(current.Object, "spec", field)
								if err != nil || value != names[i%tenants] {
									b.Fatal("class escaped the selected namespace profile")
								}
							} else if response != nil {
								b.Fatal("expected mutation skip")
							}
						}
						b.ReportMetric(0, "class-reads/op")
					})
				}
			}
		}
	}
}
