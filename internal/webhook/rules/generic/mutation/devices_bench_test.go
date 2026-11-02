// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package mutation

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	admissionv1 "k8s.io/api/admission/v1"
	resourcev1 "k8s.io/api/resource/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/internal/cache"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/api/rules"
	"github.com/projectcapsule/capsule/pkg/runtime/events"
)

func BenchmarkDeviceClassDefaultMutation(b *testing.B) {
	for _, count := range []int{1, 32} {
		for _, tenants := range []int{1, 16} {
			for _, mode := range []string{"default", "explicit", "condition-warm", "condition-cold", "condition-false", "condition-error", "max-requests", "max-alternatives"} {
				b.Run(fmt.Sprintf("rules=%d/tenants=%d/%s", count, tenants, mode), func(b *testing.B) {
					compiler, err := cache.NewCELCache()
					require.NoError(b, err)
					requests := []any{map[string]any{"name": "gpu", "exactly": map[string]any{}}}
					if mode == "explicit" {
						requests[0].(map[string]any)["exactly"] = map[string]any{"deviceClassName": "explicit"}
					}
					if mode == "max-requests" || mode == "max-alternatives" {
						requests = make([]any, resourcev1.DeviceRequestsMaxSize)
						for i := range requests {
							request := map[string]any{"name": fmt.Sprintf("gpu-%d", i), "exactly": map[string]any{}}
							if mode == "max-alternatives" {
								delete(request, "exactly")
								alternatives := make([]any, resourcev1.FirstAvailableDeviceRequestMaxSize)
								for j := range alternatives {
									alternatives[j] = map[string]any{"name": fmt.Sprintf("alternative-%d", j)}
								}
								request["firstAvailable"] = alternatives
							}
							requests[i] = request
						}
					}
					obj := deviceDefaultObject("v1", "ResourceClaim", requests)
					req := listenerSetRequest(b, obj, admissionv1.Create, "")
					profiles := make([][]*rules.NamespaceRuleBodyNamespace, tenants)
					for tenant := range profiles {
						for range count {
							mutation := deviceDefault(fmt.Sprintf("tenant-%d-gpu", tenant))
							switch mode {
							case "condition-warm", "condition-cold":
								mutation.Conditions = []rules.AdmissionCondition{{Expression: `request.operation == 'CREATE'`}}
							case "condition-false":
								mutation.Conditions = []rules.AdmissionCondition{{Expression: "false"}}
							case "condition-error":
								mutation.Conditions = []rules.AdmissionCondition{{Expression: "object.missing == true"}}
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
						current := obj.DeepCopy()
						response := h.OnCreate(nil, nil, current, nil, nil, nil, profiles[i%tenants])(b.Context(), req)
						switch mode {
						case "explicit", "condition-false":
							if response != nil {
								b.Fatal("expected unchanged claim")
							}
						case "condition-error":
							if response == nil || response.Allowed {
								b.Fatal("expected condition failure")
							}
						default:
							if response == nil || !response.Allowed || len(response.Patches) == 0 {
								b.Fatal("expected class default patch")
							}
						}
					}
				})
			}
		}
	}
}

func BenchmarkDeviceClassDefaultRegistration(b *testing.B) {
	for _, tenants := range []int{1, 100} {
		b.Run(fmt.Sprintf("tenants=%d", tenants), func(b *testing.B) {
			c, reads, decoder := mutationRegistrationFixture(b, tenants)
			status := &capsulev1beta2.RuleStatus{}
			require.NoError(b, c.Get(b.Context(), client.ObjectKey{Namespace: "selected", Name: meta.NameForManagedRuleStatus()}, status))
			status.Spec = []*rules.NamespaceRuleBodyNamespace{{Mutate: []rules.NamespaceRuleMutation{deviceDefault("gpu")}}}
			status.Status.Rules = status.Spec
			require.NoError(b, c.Update(b.Context(), status))
			obj := deviceDefaultObject("v1", "ResourceClaim", []any{map[string]any{"name": "gpu", "exactly": map[string]any{}}})
			req := listenerSetRequest(b, obj, admissionv1.Create, "")
			h := Register(nil, nil).GetHandlers()[0]
			reads.Store(0)
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				response := h.OnCreate(c, c, decoder, events.NewDiscardRecorder())(b.Context(), req)
				if response == nil || !response.Allowed || len(response.Patches) == 0 {
					b.Fatal("expected class default patch")
				}
			}
			b.StopTimer()
			b.ReportMetric(float64(reads.Load())/float64(b.N), "GET/op")
		})
	}
}

func BenchmarkDeviceClassDefaultConcurrent(b *testing.B) {
	compiler, err := cache.NewCELCache()
	require.NoError(b, err)
	obj := deviceDefaultObject("v1", "ResourceClaim", []any{map[string]any{"name": "gpu", "exactly": map[string]any{}}})
	req := listenerSetRequest(b, obj, admissionv1.Create, "")
	mutation := deviceDefault("gpu")
	mutation.Conditions = []rules.AdmissionCondition{{Expression: `request.operation == 'CREATE'`}}
	bodies := []*rules.NamespaceRuleBodyNamespace{{Mutate: []rules.NamespaceRuleMutation{mutation}}}
	h := MetadataRules(compiler)
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			response := h.OnCreate(nil, nil, obj.DeepCopy(), nil, nil, nil, bodies)(b.Context(), req)
			if response == nil || !response.Allowed || len(response.Patches) == 0 {
				b.Error("expected class default patch")
			}
		}
	})
}
