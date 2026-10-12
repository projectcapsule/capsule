// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package webhook

import (
	"fmt"
	"testing"

	"github.com/projectcapsule/capsule/pkg/api/rules"
)

// Measures the complete registered rule chain with cached regexes and fake API
// reads. Event delivery is discarded in both baselines; real writes are covered
// by router tests and tenant e2e. These timings exclude API-server latency.
func BenchmarkRouterRules(b *testing.B) {
	for _, tenants := range []int{1, 100} {
		for _, ruleCount := range []int{1, 32} {
			for _, outcome := range []string{"allow", "audit", "deny", "skip"} {
				for _, dryRun := range []bool{false, true} {
					for _, concurrent := range []bool{false, true} {
						b.Run(fmt.Sprintf("tenants=%d/rules=%d/%s/dryRun=%v/parallel=%v", tenants, ruleCount, outcome, dryRun, concurrent), func(b *testing.B) {
							action := rules.ActionType(outcome)
							if outcome == "skip" {
								action = rules.ActionTypeAudit
							}
							router, cl, requests := rulesRouterFixture(b, tenants, ruleCount, action, action)
							for i := range requests {
								requests[i].DryRun = &dryRun
								if outcome == "skip" {
									requests[i].SubResource = "unrelated"
								}
								router.Handle(b.Context(), requests[i])
							}
							cl.gets.Store(0)
							cl.lists.Store(0)
							run := func(i int) {
								index := i % len(requests)
								got := router.Handle(b.Context(), requests[index])
								if got.Allowed != (outcome != "deny" || index%2 != 0) {
									b.Fatal("unexpected admission decision")
								}
							}
							b.ReportAllocs()
							b.ResetTimer()
							if concurrent {
								b.RunParallel(func(pb *testing.PB) {
									for i := 0; pb.Next(); i++ {
										run(i)
									}
								})
							} else {
								for i := 0; i < b.N; i++ {
									run(i)
								}
							}
							b.StopTimer()
							expectedGets := int64(3 * b.N)
							if outcome == "skip" {
								expectedGets = 0
							}
							if cl.gets.Load() != expectedGets || cl.lists.Load() != 0 {
								b.Fatalf("unexpected reads: GET=%d LIST=%d", cl.gets.Load(), cl.lists.Load())
							}
							b.ReportMetric(float64(cl.gets.Load())/float64(b.N), "GET/op")
							b.ReportMetric(float64(cl.lists.Load())/float64(b.N), "LIST/op")
						})
					}
				}
			}
		}
	}
}
