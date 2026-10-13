// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package serviceaccounts

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	admissionv1 "k8s.io/api/admission/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/meta"
)

type referenceCountingClient struct {
	client.Client
	lists     int
	err       error
	unindexed bool
}

func (c *referenceCountingClient) List(ctx context.Context, list client.ObjectList, options ...client.ListOption) error {
	c.lists++
	opts := &client.ListOptions{}
	opts.ApplyOptions(options)
	if opts.FieldSelector == nil || opts.FieldSelector.Empty() {
		c.unindexed = true
	}
	if c.err != nil {
		return c.err
	}
	return c.Client.List(ctx, list, options...)
}

func TestReferenceProtectionReadBudget(t *testing.T) {
	for _, op := range []admissionv1.Operation{admissionv1.Create, admissionv1.Update, admissionv1.Delete} {
		t.Run(string(op), func(t *testing.T) {
			c := &referenceCountingClient{Client: referenceProtectionFakeClient(t)}
			h := ReferenceProtection()
			fn := h.OnDelete(c, c, admission.NewDecoder(c.Scheme()), nil)
			want := 2
			if op == admissionv1.Create {
				fn = h.OnCreate(c, c, admission.NewDecoder(c.Scheme()), nil)
				want = 0
			}
			if op == admissionv1.Update {
				fn = h.OnUpdate(c, c, admission.NewDecoder(c.Scheme()), nil)
				want = 0
			}
			response := fn(t.Context(), admission.Request{AdmissionRequest: admissionv1.AdmissionRequest{Operation: op, Namespace: "team-a", Name: "runner"}})
			if response != nil || c.lists != want || c.unindexed {
				t.Fatalf("response=%v indexed=%t lists=%d, want %d", response, !c.unindexed, c.lists, want)
			}
		})
	}
	c := &referenceCountingClient{Client: referenceProtectionFakeClient(t), err: errors.New("reference lookup failed")}
	response := ReferenceProtection().OnDelete(c, c, admission.NewDecoder(c.Scheme()), nil)(t.Context(), admission.Request{AdmissionRequest: admissionv1.AdmissionRequest{Operation: admissionv1.Delete, Namespace: "team-a", Name: "runner"}})
	if response == nil || response.Allowed || response.Result == nil || !strings.Contains(response.Result.Message, "reference lookup failed") {
		t.Fatalf("lookup error became an allow: %v", response)
	}
}

// Fake-client timing includes its selector scan; production uses the registered
// cache index. The read budget proves the handler does not add per-item reads.
func BenchmarkServiceAccountReferences(b *testing.B) {
	for _, mode := range []string{"allow", "deny", "skip"} {
		for _, count := range []int{0, 128} {
			b.Run(fmt.Sprintf("%s/unrelated=%d", mode, count), func(b *testing.B) {
				objects := []client.Object{&capsulev1beta2.Tenant{Name: "tenant-a"}, &capsulev1beta2.Tenant{Name: "tenant-b"}}
				for i := range count {
					objects = append(objects, &capsulev1beta2.GlobalTenantResource{Name: fmt.Sprintf("other-%d", i), Status: capsulev1beta2.GlobalTenantResourceStatus{TenantResourceCommonStatus: capsulev1beta2.TenantResourceCommonStatus{ServiceAccount: &meta.NamespacedRFC1123ObjectReferenceWithNamespace{Name: "runner", Namespace: "team-b"}}}})
				}
				if mode == "deny" {
					objects = append(objects, &capsulev1beta2.GlobalTenantResource{Name: "active", Status: capsulev1beta2.GlobalTenantResourceStatus{TenantResourceCommonStatus: capsulev1beta2.TenantResourceCommonStatus{ServiceAccount: &meta.NamespacedRFC1123ObjectReferenceWithNamespace{Name: "runner", Namespace: "team-a"}}}})
				}
				c := &referenceCountingClient{Client: referenceProtectionFakeClient(b, objects...)}
				h := ReferenceProtection()
				fn := h.OnDelete(c, c, admission.NewDecoder(c.Scheme()), nil)
				op := admissionv1.Delete
				if mode == "skip" {
					fn = h.OnUpdate(c, c, admission.NewDecoder(c.Scheme()), nil)
					op = admissionv1.Update
				}
				req := admission.Request{AdmissionRequest: admissionv1.AdmissionRequest{Operation: op, Namespace: "team-a", Name: "runner"}}
				b.ReportAllocs()
				for b.Loop() {
					response := fn(b.Context(), req)
					denied := response != nil && !response.Allowed
					if denied != (mode == "deny") {
						b.Fatalf("unexpected admission response: %v", response)
					}
				}
				if c.unindexed {
					b.Fatal("unindexed list")
				}
				b.ReportMetric(float64(c.lists)/float64(b.N), "lists/op")
			})
		}
	}
}
