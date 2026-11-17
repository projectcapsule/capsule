// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package mutation

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	admissionv1 "k8s.io/api/admission/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/internal/cache"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/api/rules"
	"github.com/projectcapsule/capsule/pkg/runtime/events"
)

func BenchmarkAttributeClassDefaultMutation(b *testing.B) {
	modes := []string{"merge", "replace", "explicit", "empty", "condition-warm", "condition-cold", "condition-false", "condition-error", "no-storage-rules"}

	for _, count := range []int{1, 32} {
		for _, tenants := range []int{1, 16} {
			for _, mode := range modes {
				b.Run(fmt.Sprintf("rules=%d/tenants=%d/%s", count, tenants, mode), func(b *testing.B) {
					compiler, err := cache.NewCELCache()
					require.NoError(b, err)

					var current any = omitted{}

					switch mode {
					case "explicit":
						current = "silver"
					case "empty":
						current = ""
					}

					obj := claimObject(current)
					req := listenerSetRequest(b, obj, admissionv1.Create, "")

					profiles := make([][]*rules.NamespaceRuleBodyNamespace, tenants)
					for tenant := range profiles {
						for range count {
							mutation := attributeClassDefault("", fmt.Sprintf("tenant-%d-gold", tenant))

							switch mode {
							case "replace":
								mutation.Action = rules.MutationActionReplace
							case "condition-warm", "condition-cold":
								mutation.Conditions = []rules.AdmissionCondition{{Expression: `object.metadata.labels.tier == "fast"`}}
							case "condition-false":
								mutation.Conditions = []rules.AdmissionCondition{{Expression: `object.metadata.labels.tier == "slow"`}}
							case "condition-error":
								mutation.Conditions = []rules.AdmissionCondition{{Expression: "object.missing == true"}}
							case "no-storage-rules":
								mutation = deviceDefault("gpu")
							}

							profiles[tenant] = append(profiles[tenant], &rules.NamespaceRuleBodyNamespace{Mutate: []rules.NamespaceRuleMutation{mutation}})
						}
					}

					h := MetadataRules(compiler)

					b.ReportAllocs()

					for i := 0; b.Loop(); i++ {
						if mode == "condition-cold" {
							compiler.Reset()
						}

						response := h.OnCreate(nil, nil, obj.DeepCopy(), nil, nil, nil, profiles[i%tenants])(b.Context(), req)

						switch mode {
						case "explicit", "empty", "condition-false", "no-storage-rules":
							if response != nil {
								b.Fatal("expected an unchanged claim")
							}
						case "condition-error":
							if response == nil || response.Allowed {
								b.Fatal("expected a condition failure")
							}
						default:
							if response == nil || !response.Allowed || len(response.Patches) != 1 {
								b.Fatal("expected a class patch")
							}
						}
					}
				})
			}
		}
	}
}

func BenchmarkAttributeClassDefaultRegistration(b *testing.B) {
	for _, tenants := range []int{1, 100} {
		b.Run(fmt.Sprintf("tenants=%d", tenants), func(b *testing.B) {
			c, reads, decoder := mutationRegistrationFixture(b, tenants)

			status := &capsulev1beta2.RuleStatus{}
			require.NoError(b, c.Get(b.Context(), client.ObjectKey{Namespace: "selected", Name: meta.NameForManagedRuleStatus()}, status))
			status.Spec = []*rules.NamespaceRuleBodyNamespace{{Mutate: []rules.NamespaceRuleMutation{attributeClassDefault("", "gold")}}}
			status.Status.Rules = status.Spec
			require.NoError(b, c.Update(b.Context(), status))

			req := listenerSetRequest(b, claimObject(omitted{}), admissionv1.Create, "")
			h := Register(nil, nil).GetHandlers()[0]

			reads.Store(0)
			b.ReportAllocs()
			b.ResetTimer()

			for b.Loop() {
				response := h.OnCreate(c, c, decoder, events.NewDiscardRecorder())(b.Context(), req)
				if response == nil || !response.Allowed || len(response.Patches) != 1 {
					b.Fatal("expected a class patch")
				}
			}

			b.StopTimer()
			b.ReportMetric(float64(reads.Load())/float64(b.N), "GET/op")
		})
	}
}

func BenchmarkAttributeClassDefaultConcurrent(b *testing.B) {
	compiler, err := cache.NewCELCache()
	require.NoError(b, err)

	obj := claimObject(omitted{})
	req := listenerSetRequest(b, obj, admissionv1.Create, "")
	mutation := attributeClassDefault("", "gold")
	mutation.Conditions = []rules.AdmissionCondition{{Expression: `request.operation == 'CREATE'`}}
	bodies := []*rules.NamespaceRuleBodyNamespace{{Mutate: []rules.NamespaceRuleMutation{mutation}}}
	h := MetadataRules(compiler)

	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			response := h.OnCreate(nil, nil, obj.DeepCopy(), nil, nil, nil, bodies)(b.Context(), req)
			if response == nil || !response.Allowed || len(response.Patches) != 1 {
				b.Error("expected a class patch")
			}
		}
	})
}
