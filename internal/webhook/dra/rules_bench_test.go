// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package dra

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/internal/cache"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/api/rules"
	"github.com/projectcapsule/capsule/pkg/runtime/events"
)

func BenchmarkDeviceClassRulesAdmission(b *testing.B) {
	for _, tenants := range []int{1, 100} {
		for _, count := range []int{1, 32} {
			for _, mode := range []string{"exact", "regex-warm", "regex-cold", "selector", "deny", "skip", "parallel"} {
				b.Run(fmt.Sprintf("tenants=%d/rules=%d/%s", tenants, count, mode), func(b *testing.B) {
					cl := newDRAClient(b)
					for i := 0; i < tenants-1; i++ {
						require.NoError(b, cl.Create(b.Context(), &capsulev1beta2.Tenant{Name: fmt.Sprintf("unrelated-%d", i)}))
					}
					var bodies []*rules.NamespaceRuleBodyNamespace
					for i := range count {
						match := rules.DeviceClassMatch{Exact: []string{fmt.Sprintf("unused-%d", i)}}
						if i == count-1 {
							switch mode {
							case "exact", "skip":
								match.Exact = []string{"gpu-a"}
							case "selector":
								match = rules.DeviceClassMatch{Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"tenant": "a"}}}
							default:
								match = rules.DeviceClassMatch{Expression: "^gpu-[ab]$"}
							}
						}
						bodies = append(bodies, classBody(rules.ActionTypeAllow, match).NamespaceRuleBodyNamespace)
					}
					setClassRules(b, cl, "tenant-a")
					require.NoError(b, cl.Create(b.Context(), &capsulev1beta2.RuleStatus{Name: meta.NameForManagedRuleStatus(), Namespace: "ns-a", Generation: 1, Spec: bodies, Status: capsulev1beta2.RuleStatusStatus{ObservedGeneration: 1, Rules: bodies}}))
					refs := alternativeRequests("gpu-a", "gpu-a")
					if mode == "deny" {
						refs = alternativeRequests("gpu-a", "gpu-other")
					}
					ns := "ns-a"
					if mode == "skip" {
						ns = "unmanaged"
					}
					request := draAdmissionRequest(b, "ResourceClaim", ns, refs)
					regex := cache.NewRegexCache()
					handler := DeviceClass(nil, regex, nil, nil).OnCreate(cl, cl, admission.NewDecoder(cl.Scheme()), events.NewDiscardRecorder())
					run := func() {
						response := handler(b.Context(), request)
						if mode == "deny" {
							if response == nil || response.Allowed {
								b.Error("expected denial")
							}
						} else if response != nil {
							b.Errorf("unexpected response: %+v", response)
						}
					}
					run()
					cl.deviceGets.Store(0)
					cl.tenantLists.Store(0)
					cl.tenantGets.Store(0)
					cl.namespaceGets.Store(0)
					cl.rulesetGets.Store(0)
					b.ReportAllocs()
					b.ResetTimer()
					if mode == "parallel" {
						b.RunParallel(func(pb *testing.PB) {
							for pb.Next() {
								run()
							}
						})
					} else {
						for range b.N {
							if mode == "regex-cold" {
								regex.Reset()
							}
							run()
						}
					}
					b.StopTimer()
					b.ReportMetric(float64(cl.deviceGets.Load())/float64(b.N), "class-gets/op")
					b.ReportMetric(float64(cl.tenantLists.Load())/float64(b.N), "tenant-lists/op")
					b.ReportMetric(float64(cl.tenantGets.Load()+cl.namespaceGets.Load()+cl.rulesetGets.Load())/float64(b.N), "profile-gets/op")
				})
			}
		}
	}
}
