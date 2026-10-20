// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package tenant

import (
	"fmt"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	mockclient "github.com/projectcapsule/capsule/internal/mocks/client"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	capsuletenant "github.com/projectcapsule/capsule/pkg/tenant"
)

func BenchmarkLegacyQuotaRecount(b *testing.B) {
	benchmarkLegacyQuotaRecount(b, false)
}

func BenchmarkLegacyQuotaRecoveryQueue(b *testing.B) {
	benchmarkLegacyQuotaRecount(b, true)
}

func benchmarkLegacyQuotaRecount(b *testing.B, queue bool) {
	for _, unrelated := range []int{0, 127} {
		for _, namespaces := range []int{1, 16} {
			for _, coordination := range []struct{ pending, deleting int }{{}, {pending: 16}, {deleting: 16}, {pending: 16, deleting: 16}} {
				b.Run(fmt.Sprintf("unrelatedTenants=%d/namespaces=%d/pending=%d/deleting=%d", unrelated, namespaces, coordination.pending, coordination.deleting), func(b *testing.B) {
					tnt := provisioningTenant()
					tnt.Annotations = map[string]string{capsulev1beta2.LimitAnnotationForResource(recountResource): "500"}
					state, _ := capsuletenant.ReadLegacyQuotaReservations(nil)
					state.Deleting = make(map[string][]capsuletenant.LegacyQuotaDeletion)
					objects := []client.Object{}
					for i := range namespaces {
						ns := cleanupOwnedNamespace(tnt, fmt.Sprintf("space-%d", i), false)
						objects = append(objects, ns)
						tnt.Status.Namespaces = append(tnt.Status.Namespaces, ns.Name)
						tnt.Status.Spaces = append(tnt.Status.Spaces, &capsulev1beta2.TenantStatusNamespaceItem{Name: ns.Name, UID: ns.UID, Conditions: meta.ConditionList{meta.NewReadyCondition(ns)}})
						for j := range coordination.pending {
							now := metav1.Now()
							expiry := metav1.NewTime(now.Add(2 * time.Minute))
							state.Pending[recountResource] = append(state.Pending[recountResource], capsuletenant.LegacyQuotaReservation{NamespaceUID: ns.UID, ID: fmt.Sprintf("%d-%d", i, j), CreatedAt: now, ExpiresAt: &expiry, ObjectRef: capsulev1beta2.QuantityLedgerObjectRef{Namespace: ns.Name, Name: fmt.Sprintf("pending-%d", j)}})
						}
						for j := range coordination.deleting {
							now := metav1.Now()
							expiry := metav1.NewTime(now.Add(2 * time.Minute))
							state.Deleting[recountResource] = append(state.Deleting[recountResource], capsuletenant.LegacyQuotaDeletion{NamespaceUID: ns.UID, ID: fmt.Sprintf("delete-%d-%d", i, j), CreatedAt: now, ExpiresAt: &expiry, ObjectRef: capsulev1beta2.QuantityLedgerObjectRef{Namespace: ns.Name, Name: fmt.Sprintf("widget-%d", j), UID: "persisted"}})
						}
					}
					if err := capsuletenant.WriteLegacyQuotaReservations(tnt.Annotations, state); err != nil {
						b.Fatal(err)
					}
					objects = append(objects, tnt)
					for i := range unrelated {
						foreign := &capsulev1beta2.Tenant{Name: fmt.Sprintf("unrelated-%d", i)}
						objects = append(objects, foreign, cleanupOwnedNamespace(foreign, fmt.Sprintf("foreign-%d", i), false))
					}
					r, dyn := namespaceCleanupFixture(b, objects...)
					for _, name := range tnt.Status.Namespaces {
						for j := range 100 {
							obj := &unstructured.Unstructured{Object: map[string]any{"apiVersion": "example.com/v1", "kind": "Widget", "metadata": map[string]any{"namespace": name, "name": fmt.Sprintf("widget-%d", j), "uid": "persisted"}}}
							if _, err := dyn.Resource(recountGVR).Namespace(name).Create(b.Context(), obj, metav1.CreateOptions{}); err != nil {
								b.Fatal(err)
							}
						}
					}
					calls := &mockclient.CallCounter{}
					c := interceptor.NewClient(r.Client.(client.WithWatch), calls.Interceptors())
					r.Client, r.reader = c, c
					run := func() error { return r.syncCustomResourceQuotaUsages(b.Context(), tnt) }
					if queue {
						run = func() error {
							_, err := r.reconcileResourceQuotas(b.Context(), reconcile.Request{Name: tnt.Name})
							return err
						}
					}
					if err := run(); err != nil {
						b.Fatal(err)
					}
					calls.Reset()
					b.ReportAllocs()
					b.ResetTimer()
					for b.Loop() {
						if err := run(); err != nil {
							b.Fatal(err)
						}
					}
					calls.Report(b)
				})
			}
		}
	}
}
