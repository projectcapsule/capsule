// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package generic

import (
	"context"
	"fmt"
	"testing"
	"time"

	admissionv1 "k8s.io/api/admission/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	mockclient "github.com/projectcapsule/capsule/internal/mocks/client"
	"github.com/projectcapsule/capsule/pkg/tenant"
)

func BenchmarkLegacyQuotaAdmissionPending(b *testing.B) {
	for _, pending := range []int{16, 128} {
		for _, parallel := range []bool{false, true} {
			b.Run(fmt.Sprintf("pending=%d/parallel=%v", pending, parallel), func(b *testing.B) {
				base := legacyQuotaFixture(b, 16)
				tnt := &capsulev1beta2.Tenant{}
				if err := base.Get(b.Context(), client.ObjectKey{Name: "tenant-0"}, tnt); err != nil {
					b.Fatal(err)
				}
				state, _ := tenant.ReadLegacyQuotaReservations(nil)
				for i := range pending {
					now := metav1.Now()
					expiry := metav1.NewTime(now.Add(2 * time.Minute))
					state.Pending[quotaTestResource] = append(state.Pending[quotaTestResource], tenant.LegacyQuotaReservation{NamespaceUID: "tenant-0-ns", ID: fmt.Sprintf("request-%d", i), CreatedAt: now, ExpiresAt: &expiry, ObjectRef: capsulev1beta2.QuantityLedgerObjectRef{Namespace: "tenant-0-ns", Name: fmt.Sprintf("widget-%d", i)}})
				}
				if err := tenant.WriteLegacyQuotaReservations(tnt.Annotations, state); err != nil {
					b.Fatal(err)
				}
				tnt.Annotations[capsulev1beta2.LimitAnnotationForResource(quotaTestResource)] = "500"
				tnt.Annotations[capsulev1beta2.UsedAnnotationForResource(quotaTestResource)] = fmt.Sprint(pending)
				if err := base.Update(b.Context(), tnt); err != nil {
					b.Fatal(err)
				}
				calls := &mockclient.CallCounter{}
				writer := interceptor.NewClient(base, interceptor.Funcs{Update: func(context.Context, client.WithWatch, client.Object, ...client.UpdateOption) error { return nil }})
				c := interceptor.NewClient(writer, calls.Interceptors())
				req := legacyQuotaRequest(admissionv1.Create)
				req.UID = "next-request"
				run := func() {
					if result := invokeLegacyQuota(b.Context(), c, c, req); result != nil {
						b.Errorf("unexpected response: %+v", result)
					}
				}
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
