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

func BenchmarkListenerSetMutation(b *testing.B) {
	for _, count := range []int{1, 32} {
		for _, tenants := range []int{1, 16} {
			for _, state := range []string{"default", "replace", "preserve", "skip-condition", "condition-error", "status"} {
				b.Run(fmt.Sprintf("rules=%d/tenants=%d/%s", count, tenants, state), func(b *testing.B) {
					compiler, err := cache.NewCELCache()
					require.NoError(b, err)
					obj := listenerSetObject()
					listeners := make([]any, count)
					for i := range listeners {
						listeners[i] = map[string]any{"name": fmt.Sprintf("http-%d", i), "port": int64(80), "protocol": "HTTP", "hostname": fmt.Sprintf("app-%d.example.com", i)}
					}
					require.NoError(b, unstructured.SetNestedSlice(obj.Object, listeners, "spec", "listeners"))
					if state == "replace" || state == "preserve" {
						obj.Object["spec"].(map[string]any)["parentRef"] = map[string]any{"name": "explicit"}
					}
					req := listenerSetRequest(b, obj, admissionv1.Create, "")
					if state == "status" {
						req.Operation, req.SubResource = admissionv1.Update, "status"
					}
					profiles := make([][]*rules.NamespaceRuleBodyNamespace, tenants)
					names := make([]string, tenants)
					for tenant := range profiles {
						names[tenant] = fmt.Sprintf("tenant-%d", tenant)
						for range count {
							mutation := listenerSetMutation(names[tenant], rules.MutationActionMerge)
							if state == "replace" {
								mutation.Action = rules.MutationActionReplace
							}
							if state == "skip-condition" {
								mutation.Conditions = []rules.AdmissionCondition{{Expression: "false"}}
							}
							if state == "condition-error" {
								mutation.Conditions = []rules.AdmissionCondition{{Expression: "object.missing == true"}}
							}
							profiles[tenant] = append(profiles[tenant], &rules.NamespaceRuleBodyNamespace{Mutate: []rules.NamespaceRuleMutation{mutation}})
						}
					}
					h := MetadataRules(compiler)
					b.ReportAllocs()
					for i := 0; b.Loop(); i++ {
						current := obj.DeepCopy()
						response := h.OnCreate(nil, nil, current, nil, nil, nil, profiles[i%tenants])(b.Context(), req)
						switch state {
						case "preserve", "skip-condition", "status":
							if response != nil {
								b.Fatal("expected skip")
							}
						case "condition-error":
							if response == nil || response.Allowed {
								b.Fatal("expected condition rejection")
							}
						default:
							if response == nil || !response.Allowed || len(response.Patches) == 0 {
								b.Fatal("expected parent reference patch")
							}
							ref := current.Object["spec"].(map[string]any)["parentRef"].(map[string]any)
							if ref["name"] != names[i%tenants] {
								b.Fatal("reference escaped namespace profile")
							}
						}
					}
				})
			}
		}
	}
}

func BenchmarkListenerSetMutationParallel(b *testing.B) {
	compiler, err := cache.NewCELCache()
	require.NoError(b, err)
	obj := listenerSetObject()
	req := listenerSetRequest(b, obj, admissionv1.Create, "")
	mutation := listenerSetMutation("edge", "")
	mutation.Conditions = []rules.AdmissionCondition{{Expression: `request.operation == 'CREATE'`}}
	bodies := []*rules.NamespaceRuleBodyNamespace{{Mutate: []rules.NamespaceRuleMutation{mutation}}}
	h := MetadataRules(compiler)
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			current := obj.DeepCopy()
			response := h.OnCreate(nil, nil, current, nil, nil, nil, bodies)(b.Context(), req)
			if response == nil || !response.Allowed || len(response.Patches) == 0 {
				b.Error("expected parent reference patch")
			}
		}
	})
}
