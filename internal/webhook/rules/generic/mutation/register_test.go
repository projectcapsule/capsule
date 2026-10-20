// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package mutation

import (
	"context"
	"encoding/json"
	"fmt"
	"sync/atomic"
	"testing"

	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/api/rules"
	"github.com/projectcapsule/capsule/pkg/runtime/events"
	"github.com/projectcapsule/capsule/pkg/runtime/handlers"
	webhookutils "github.com/projectcapsule/capsule/pkg/runtime/webhook/utils"
)

func mutationRegistrationFixture(tb testing.TB, tenants int) (client.WithWatch, *atomic.Int64, admission.Decoder) {
	tb.Helper()
	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		tb.Fatal(err)
	}
	if err := capsulev1beta2.AddToScheme(scheme); err != nil {
		tb.Fatal(err)
	}
	tnt := &capsulev1beta2.Tenant{Name: "tenant", UID: "tenant-uid"}
	ns := &corev1.Namespace{Name: "selected", OwnerReferences: []metav1.OwnerReference{{APIVersion: capsulev1beta2.GroupVersion.String(), Kind: "Tenant", Name: tnt.Name, UID: tnt.UID}}}
	status := &capsulev1beta2.RuleStatus{Name: meta.NameForManagedRuleStatus(), Namespace: ns.Name, Generation: 1,
		Status: capsulev1beta2.RuleStatusStatus{ObservedGeneration: 1, Rules: []*rules.NamespaceRuleBodyNamespace{{Enforce: &rules.NamespaceRuleEnforceBody{
			Action: rules.ActionTypeAllow, Metadata: []rules.MetadataRule{{Kinds: []string{"ConfigMap"}, Labels: map[string]rules.MetadataValueRule{"managed": {Managed: new("value")}}}},
		}}}},
	}
	objects := []client.Object{tnt, ns, status}
	for i := 1; i < tenants; i++ {
		objects = append(objects, &capsulev1beta2.Tenant{Name: fmt.Sprintf("other-%d", i)})
	}
	base := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objects...).Build()
	reads := &atomic.Int64{}
	c := interceptor.NewClient(base, interceptor.Funcs{Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
		reads.Add(1)
		return c.Get(ctx, key, obj, opts...)
	}})
	return c, reads, admission.NewDecoder(scheme)
}

func mutationRegistrationRequest(tb testing.TB, kind, subresource string) admission.Request {
	tb.Helper()
	object := map[string]any{"apiVersion": "v1", "kind": kind, "metadata": map[string]any{"name": "object", "namespace": "selected"}}
	if kind == "Pod" {
		object["spec"] = map[string]any{"containers": []any{map[string]any{"name": "app", "image": "nginx"}}}
	}
	if kind == "ConfigMap" {
		object["data"] = map[string]any{"payload": "value"}
	}
	raw, err := json.Marshal(object)
	if err != nil {
		tb.Fatal(err)
	}
	return admission.Request{Operation: admissionv1.Update, Kind: metav1.GroupVersionKind{Version: "v1", Kind: kind}, Namespace: "selected", Name: "object", SubResource: subresource, Object: runtime.RawExtension{Raw: raw}, OldObject: runtime.RawExtension{Raw: raw}}
}

func TestMutationRegistrationSkipsBeforeReadsAndDecoding(t *testing.T) {
	for _, resource := range []struct{ kind, subresource string }{{"Pod", "status"}, {"Namespace", "status"}, {"Deployment", "scale"}, {"Pod", "binding"}} {
		t.Run(resource.kind+"/"+resource.subresource, func(t *testing.T) {
			c, reads, decoder := mutationRegistrationFixture(t, 2)
			h := Register(nil, nil).GetHandlers()[0]
			req := mutationRegistrationRequest(t, resource.kind, resource.subresource)
			req.Object.Raw, req.OldObject.Raw = []byte("invalid JSON"), []byte("invalid JSON")
			for _, fn := range []handlers.Func{h.OnCreate(c, c, decoder, events.NewDiscardRecorder()), h.OnUpdate(c, c, decoder, events.NewDiscardRecorder()), h.OnDelete(c, c, decoder, events.NewDiscardRecorder())} {
				if response := fn(t.Context(), req); response != nil {
					t.Fatalf("skip must continue chain: %+v", response)
				}
			}
			if reads.Load() != 0 {
				t.Fatalf("irrelevant request performed %d API reads", reads.Load())
			}
		})
	}
}

func TestMutationRegistrationRetainsRelevantRequests(t *testing.T) {
	for _, tc := range []struct {
		name, kind, subresource string
		invalid                 bool
	}{
		{"main mutation", "ConfigMap", "", false}, {"ephemeral update", "Pod", "ephemeralcontainers", false}, {"malformed main", "ConfigMap", "", true}, {"incomplete kind", "", "status", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, reads, decoder := mutationRegistrationFixture(t, 2)
			req := mutationRegistrationRequest(t, tc.kind, tc.subresource)
			if tc.invalid {
				req.Object.Raw = []byte("invalid JSON")
			}
			h := Register(nil, nil).GetHandlers()[0]
			response := h.OnUpdate(c, c, decoder, events.NewDiscardRecorder())(t.Context(), req)
			if reads.Load() == 0 {
				t.Fatal("relevant request bypassed ruleset resolution")
			}
			if tc.invalid || tc.kind == "" {
				if response == nil || response.Allowed {
					t.Fatalf("invalid request accepted: %+v", response)
				}
			} else if tc.kind == "ConfigMap" {
				if response == nil || !response.Allowed || len(response.Patches) == 0 {
					t.Fatalf("managed label was not applied: %+v", response)
				}
			} else if response != nil && !response.Allowed {
				t.Fatalf("ephemeral request denied: %+v", response)
			}
		})
	}
}

func BenchmarkMutationRegistration(b *testing.B) {
	for _, tenants := range []int{1, 16} {
		for _, state := range []string{"skip", "allow", "deny", "ephemeral"} {
			for _, filtered := range []bool{false, true} {
				b.Run(fmt.Sprintf("tenants=%d/%s/filtered=%t", tenants, state, filtered), func(b *testing.B) {
					c, reads, decoder := mutationRegistrationFixture(b, tenants)
					req := mutationRegistrationRequest(b, "ConfigMap", "")
					if state == "skip" {
						req = mutationRegistrationRequest(b, "Pod", "status")
					}
					if state == "ephemeral" {
						req = mutationRegistrationRequest(b, "Pod", "ephemeralcontainers")
					}
					if state == "deny" {
						req.Object.Raw = []byte("invalid JSON")
					}
					h := genericHandler(nil, MetadataRules(nil))
					if filtered {
						h = Register(nil, nil).GetHandlers()[0]
					}
					b.ReportAllocs()
					b.ResetTimer()
					for b.Loop() {
						reader := webhookutils.NewRequestCachingReader(c)
						response := h.OnUpdate(c, reader, decoder, events.NewDiscardRecorder())(b.Context(), req)
						if state == "deny" && (response == nil || response.Allowed) {
							b.Fatal("invalid JSON accepted")
						}
					}
					b.ReportMetric(float64(reads.Load())/float64(b.N), "GET/op")
				})
			}
		}
	}
}
