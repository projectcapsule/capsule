// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package validation

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	networkingv1 "k8s.io/api/networking/v1"

	genericvalidation "github.com/projectcapsule/capsule/internal/webhook/rules/generic/validation"
	"github.com/projectcapsule/capsule/pkg/api/rules"
)

func BenchmarkEgressCIDR(b *testing.B) {
	for _, count := range []int{1, 16, 64} {
		for _, peers := range []int{1, 8} {
			for _, outcome := range []string{"allow", "allow-overrides", "deny", "skip"} {
				b.Run(fmt.Sprintf("rules=%d/peers=%d/%s", count, peers, outcome), func(b *testing.B) {
					var bodies []*rules.NamespaceRuleEnforceBody
					for i := range count {
						bodies = append(bodies, cidrRule(rules.ActionTypeDeny, fmt.Sprintf("10.%d.0.0/16", i)))
					}
					obj := policy("::/0")
					if outcome == "allow-overrides" {
						bodies[count-1] = cidrRule(rules.ActionTypeAllow, "10.0.0.0/8")
						obj = policy("10.0.0.0/8")
					}
					if outcome == "deny" {
						obj = policy("0.0.0.0/0", "10.0.0.0/24")
					}
					if outcome == "skip" {
						obj.Spec.Egress = nil
					}
					if len(obj.Spec.Egress) > 0 {
						peer := obj.Spec.Egress[0].To[0]
						obj.Spec.Egress[0].To = make([]networkingv1.NetworkPolicyPeer, peers)
						for i := range peers {
							obj.Spec.Egress[0].To[i] = peer
						}
					}
					b.ReportAllocs()
					for b.Loop() {
						result, err := evaluate(b.Context(), obj, bodies)
						if err != nil || (result.BlockingError() != nil) != (outcome == "deny") {
							b.Fatalf("unexpected result: %v, %v", result, err)
						}
					}
				})
			}
		}
	}
}

// enabled=false measures the existing generic chain on exactly the same input.
func BenchmarkNetworkPolicyAdmission(b *testing.B) {
	for _, tenants := range []int{1, 100} {
		for _, enabled := range []bool{false, true} {
			for _, outcome := range []string{"allow", "deny", "unrelated"} {
				b.Run(fmt.Sprintf("tenants=%d/enabled=%v/%s", tenants, enabled, outcome), func(b *testing.B) {
					cl, decoder, requests := admissionFixture(b, tenants, true, func(int) []*rules.NamespaceRuleBodyNamespace {
						cidr := "192.0.2.0/24"
						if outcome == "deny" {
							cidr = "10.0.0.0/8"
						}
						return []*rules.NamespaceRuleBodyNamespace{{Enforce: cidrRule(rules.ActionTypeDeny, cidr)}}
					})
					if outcome == "unrelated" {
						for i := range requests {
							requests[i].Kind.Group, requests[i].Kind.Kind = "", "ConfigMap"
						}
					}
					chain := genericvalidation.Register(nil, nil, nil, nil, nil).GetHandlers()
					if enabled {
						chain = genericvalidation.Register(nil, nil, nil, nil, nil, Handler(nil, nil)).GetHandlers()
					}
					b.ReportAllocs()
					for i := 0; b.Loop(); i++ {
						response := runChain(b.Context(), cl, decoder, chain, requests[i%tenants])
						if (response != nil) != (enabled && outcome == "deny") {
							b.Fatalf("unexpected response: %v", response)
						}
					}
					require.Zero(b, cl.lists.Load())
					b.ReportMetric(float64(cl.gets.Load())/float64(b.N), "GET/op")
					b.ReportMetric(float64(cl.lists.Load())/float64(b.N), "LIST/op")
				})
			}
		}
	}
}

func BenchmarkEgressCIDRConcurrent(b *testing.B) {
	obj := policy("0.0.0.0/0", "10.20.0.0/16")
	bodies := []*rules.NamespaceRuleEnforceBody{cidrRule(rules.ActionTypeDeny, "10.20.0.0/16")}
	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			result, err := evaluate(b.Context(), obj, bodies)
			if err != nil || result.BlockingError() != nil {
				b.Errorf("unexpected validation: %v, %v", result, err)
			}
		}
	})
}

func BenchmarkEgressExceptions(b *testing.B) {
	for _, count := range []int{512, 2048, 8192} {
		b.Run(fmt.Sprint(count), func(b *testing.B) {
			obj := exceptionPolicy(count)
			bodies := []*rules.NamespaceRuleEnforceBody{cidrRule(rules.ActionTypeDeny, "192.0.2.0/24")}
			b.ReportAllocs()
			for b.Loop() {
				result, err := evaluate(b.Context(), obj, bodies)
				if err != nil || result.BlockingError() != nil {
					b.Fatalf("unexpected result: %v, %v", result, err)
				}
			}
		})
	}
}

func BenchmarkEgressOverlappingAudits(b *testing.B) {
	for _, count := range []int{16, 64} {
		for _, peers := range []int{1, 8, 32} {
			b.Run(fmt.Sprintf("cidrs=%d/peers=%d", count, peers), func(b *testing.B) {
				obj, bodies := auditPolicy(count, peers)
				b.ReportAllocs()
				for b.Loop() {
					result, err := evaluate(b.Context(), obj, bodies)
					if err != nil || result.BlockingError() != nil || len(result.Audits) != count*peers {
						b.Fatalf("unexpected audit result: %v, %v", result, err)
					}
				}
			})
		}
	}
}

func BenchmarkEgressStressConcurrent(b *testing.B) {
	for _, audit := range []bool{false, true} {
		b.Run(fmt.Sprintf("audit=%v", audit), func(b *testing.B) {
			obj := exceptionPolicy(8192)
			bodies := []*rules.NamespaceRuleEnforceBody{cidrRule(rules.ActionTypeDeny, "192.0.2.0/24")}
			if audit {
				obj, bodies = auditPolicy(64, 8)
			}
			b.ReportAllocs()
			b.ResetTimer()
			b.RunParallel(func(pb *testing.PB) {
				for pb.Next() {
					result, err := evaluate(b.Context(), obj, bodies)
					if err != nil || result.BlockingError() != nil {
						b.Errorf("unexpected result: %v, %v", result, err)
					}
				}
			})
		})
	}
}
