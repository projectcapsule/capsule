// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package webhook

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/internal/cache"
	rulesmutation "github.com/projectcapsule/capsule/internal/webhook/rules/generic/mutation"
	rulesvalidation "github.com/projectcapsule/capsule/internal/webhook/rules/generic/validation"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/api/rules"
	"github.com/projectcapsule/capsule/pkg/runtime/events"
)

func provisioningRouterFixture(tb testing.TB, state, route, outcome string, tenants, ruleCount int) (*handlerRouter, *routerCountingClient, []admission.Request) {
	tb.Helper()
	scheme := runtime.NewScheme()
	require.NoError(tb, corev1.AddToScheme(scheme))
	require.NoError(tb, capsulev1beta2.AddToScheme(scheme))
	var objects []client.Object
	var requests []admission.Request
	for i := range tenants {
		tnt := &capsulev1beta2.Tenant{Name: fmt.Sprintf("tenant-%d", i), UID: types.UID(fmt.Sprintf("tenant-%d-uid", i))}
		var bodies []*rules.NamespaceRuleBodyNamespace
		for j := range ruleCount {
			bodies = append(bodies, &rules.NamespaceRuleBodyNamespace{Enforce: &rules.NamespaceRuleEnforceBody{
				Action:   rules.ActionTypeAllow,
				Metadata: []rules.MetadataRule{{Kinds: []string{"ConfigMap"}, Labels: map[string]rules.MetadataValueRule{fmt.Sprintf("managed-%d", j): {Managed: new(tnt.Name)}}}},
			}})
		}
		bodies = append(bodies, &rules.NamespaceRuleBodyNamespace{Enforce: &rules.NamespaceRuleEnforceBody{
			Action:   rules.ActionTypeDeny,
			Metadata: []rules.MetadataRule{{Kinds: []string{"ConfigMap"}, Labels: map[string]rules.MetadataValueRule{"blocked": {}}}},
		}})
		for _, body := range bodies {
			tnt.Spec.Rules = append(tnt.Spec.Rules, &rules.NamespaceRuleBodyTenant{
				NamespaceRuleBodyNamespace: body,
				NamespaceSelector:          &metav1.LabelSelector{MatchLabels: map[string]string{"profile": "guarded"}},
			})
		}
		objects = append(objects, tnt)
		for _, profile := range []string{"guarded", "plain"} {
			ns := &corev1.Namespace{
				Name: tnt.Name + "-" + profile, UID: types.UID(tnt.Name + "-" + profile), Labels: map[string]string{"profile": profile},
				OwnerReferences: []metav1.OwnerReference{{APIVersion: capsulev1beta2.GroupVersion.String(), Kind: "Tenant", Name: tnt.Name, UID: tnt.UID}},
			}
			objects = append(objects, ns)
			if state != "missing" {
				rs := &capsulev1beta2.RuleStatus{Name: meta.NameForManagedRuleStatus(), Namespace: ns.Name, Generation: 2}
				if profile == "guarded" {
					rs.Spec = bodies
				}
				if state == "current" {
					rs.Status.ObservedGeneration, rs.Status.Rules = rs.Generation, rs.Spec
				}
				objects = append(objects, rs)
			}
			cm := &corev1.ConfigMap{APIVersion: "v1", Kind: "ConfigMap", Name: "example", Namespace: ns.Name}
			if outcome == "deny" {
				cm.Labels = map[string]string{"blocked": "true"}
			}
			raw, err := json.Marshal(cm)
			require.NoError(tb, err)
			old := cm.DeepCopy()
			old.Labels = nil
			oldRaw, err := json.Marshal(old)
			require.NoError(tb, err)
			req := admission.Request{Operation: admissionv1.Create, Namespace: ns.Name, Name: cm.Name, Kind: metav1.GroupVersionKind{Version: "v1", Kind: "ConfigMap"}, Object: runtime.RawExtension{Raw: raw}, OldObject: runtime.RawExtension{Raw: oldRaw}}
			if outcome == "skip" {
				req.SubResource = "status"
			}
			requests = append(requests, req)
		}
	}
	cl := &routerCountingClient{Client: fake.NewClientBuilder().WithScheme(scheme).WithObjects(objects...).Build()}
	router := &handlerRouter{client: cl, reader: cl, decoder: admission.NewDecoder(scheme), recorder: events.NewDiscardRecorder()}
	if route == "mutating" {
		router.handlers = rulesmutation.Register(nil, nil).GetHandlers()
	} else {
		router.handlers = rulesvalidation.Register(cache.NewRegexCache(), nil, nil, nil, nil).GetHandlers()
	}
	return router, cl, requests
}

func TestProvisioningRoutesReuseOwnershipSnapshot(t *testing.T) {
	for _, state := range []string{"missing", "pending", "current"} {
		for _, route := range []string{"mutating", "validating"} {
			for _, outcome := range []string{"allow", "deny", "skip"} {
				if route == "mutating" && outcome == "deny" {
					continue
				}
				t.Run(state+"/"+route+"/"+outcome, func(t *testing.T) {
					router, cl, requests := provisioningRouterFixture(t, state, route, outcome, 2, 1)
					if route == "mutating" {
						require.Len(t, router.handlers, 1, "exercise the production single-handler route")
					}
					for _, operation := range []admissionv1.Operation{admissionv1.Create, admissionv1.Update} {
						for i, req := range requests {
							req.Operation = operation
							cl.gets.Store(0)
							cl.lists.Store(0)
							response := router.Handle(t.Context(), req)
							selected := i%2 == 0
							require.Equal(t, outcome != "deny" || !selected, response.Allowed)
							if !response.Allowed {
								require.Contains(t, response.Result.Message, "blocked")
							}
							if route == "mutating" && outcome == "allow" {
								require.Equal(t, selected, len(response.Patches) > 0)
							}
							wantGets := int64(3)
							if outcome == "skip" {
								wantGets = 0
							}
							require.Equal(t, wantGets, cl.gets.Load())
							require.Zero(t, cl.lists.Load())
						}
					}
				})
			}
		}
	}
}

func BenchmarkRouterProvisioning(b *testing.B) {
	for _, state := range []string{"current", "pending"} {
		for _, tenants := range []int{1, 16} {
			for _, count := range []int{1, 32} {
				for _, route := range []string{"mutating", "validating"} {
					for _, outcome := range []string{"allow", "deny", "skip"} {
						if route == "mutating" && outcome == "deny" {
							continue
						}
						for _, parallel := range []bool{false, true} {
							b.Run(fmt.Sprintf("%s/tenants=%d/rules=%d/%s/%s/parallel=%v", state, tenants, count, route, outcome, parallel), func(b *testing.B) {
								router, cl, requests := provisioningRouterFixture(b, state, route, outcome, tenants, count)
								run := func(i int) {
									index := i % len(requests)
									response := router.Handle(b.Context(), requests[index])
									if response.Allowed != (outcome != "deny" || index%2 != 0) {
										b.Fatal("unexpected admission decision")
									}
								}
								// Warm shared compilation caches; the per-request reader is
								// still fresh for every measured admission.
								for i := range requests {
									run(i)
								}
								cl.gets.Store(0)
								cl.lists.Store(0)
								b.ReportAllocs()
								b.ResetTimer()
								if parallel {
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
								b.ReportMetric(float64(cl.gets.Load())/float64(b.N), "GET/op")
								b.ReportMetric(float64(cl.lists.Load())/float64(b.N), "LIST/op")
							})
						}
					}
				}
			}
		}
	}
}
