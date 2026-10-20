// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package generic

import (
	"context"
	"fmt"
	"testing"

	admissionv1 "k8s.io/api/admission/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	mockclient "github.com/projectcapsule/capsule/internal/mocks/client"
)

func BenchmarkLegacyQuotaAdmission(b *testing.B) {
	for _, tenants := range []int{1, 16, 128} {
		for _, outcome := range []string{"allow", "deny", "delete", "unlimited", "skip"} {
			for _, parallel := range []bool{false, true} {
				b.Run(fmt.Sprintf("tenants=%d/%s/parallel=%v", tenants, outcome, parallel), func(b *testing.B) {
					base := legacyQuotaFixture(b, tenants)
					tnt := &capsulev1beta2.Tenant{}
					if err := base.Get(b.Context(), client.ObjectKey{Name: "tenant-0"}, tnt); err != nil {
						b.Fatal(err)
					}
					// Include visible membership so the original implementation performs its
					// normal lookup; the pending-membership regression is covered separately.
					tnt.Status.Namespaces = []string{"tenant-0-ns"}
					if err := base.Status().Update(b.Context(), tnt); err != nil {
						b.Fatal(err)
					}
					switch outcome {
					case "deny":
						tnt.Annotations[capsulev1beta2.LimitAnnotationForResource(quotaTestResource)] = "0"
					case "unlimited":
						delete(tnt.Annotations, capsulev1beta2.LimitAnnotationForResource(quotaTestResource))
					}
					if err := base.Update(b.Context(), tnt); err != nil {
						b.Fatal(err)
					}
					// A stable successful reservation avoids measuring fixture resets and
					// contention from fake resource versions instead of admission work.
					writer := interceptor.NewClient(base, interceptor.Funcs{Update: func(context.Context, client.WithWatch, client.Object, ...client.UpdateOption) error { return nil }})
					calls := &mockclient.CallCounter{}
					c := interceptor.NewClient(writer, calls.Interceptors())
					req := legacyQuotaRequest(admissionv1.Create)
					req.UID = "benchmark-admission"
					if outcome == "delete" {
						req.Operation = admissionv1.Delete
					}
					if outcome == "skip" {
						req.Operation = admissionv1.Update
					}
					run := func() {
						response := invokeLegacyQuota(b.Context(), c, c, req)
						if outcome == "deny" {
							if response == nil || response.Allowed {
								b.Error("quota denial missing")
							}
						} else if response != nil {
							b.Errorf("unexpected response: %+v", response)
						}
					}
					run()
					calls.Reset()
					b.ReportAllocs()
					b.ResetTimer()
					if parallel {
						b.RunParallel(func(pb *testing.PB) {
							for pb.Next() {
								run()
							}
						})
					} else {
						for range b.N {
							run()
						}
					}
					b.StopTimer()
					calls.Report(b)
				})
			}
		}
	}
}
