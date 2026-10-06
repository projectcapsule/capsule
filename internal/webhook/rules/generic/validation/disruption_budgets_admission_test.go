// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package validation

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/internal/cache"
	webhookutils "github.com/projectcapsule/capsule/internal/webhook/utils"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/api/rbac"
	"github.com/projectcapsule/capsule/pkg/api/rules"
	"github.com/projectcapsule/capsule/pkg/runtime/configuration"
	"github.com/projectcapsule/capsule/pkg/users"
)

func overlapNamespace(tenant, namespace string, selected bool) *corev1.Namespace {
	profile := "other"
	if selected {
		profile = "pdb"
	}
	return &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespace, Labels: map[string]string{"profile": profile}, OwnerReferences: []metav1.OwnerReference{{APIVersion: capsulev1beta2.GroupVersion.String(), Kind: "Tenant", Name: tenant, UID: types.UID(tenant)}}}}
}

func TestDisruptionBudgetProfilesThroughAdmissionChain(t *testing.T) {
	scheme := overlapScheme(t)
	for _, statusPresent := range []bool{true, false} {
		t.Run(fmt.Sprintf("RuleStatus=%v", statusPresent), func(t *testing.T) {
			var objects []client.Object
			for _, name := range []string{"a", "b"} {
				body := overlapRule(rules.ActionTypeAllow, false)
				if name == "b" {
					body = overlapRule(rules.ActionTypeAllow, true)
				}
				body.Audience = []rules.Audience{{Kind: rules.AudienceKindCustom, Name: string(rules.CustomAudienceTenantOwner)}}
				tnt := &capsulev1beta2.Tenant{ObjectMeta: metav1.ObjectMeta{Name: name, UID: types.UID(name)}, Spec: capsulev1beta2.TenantSpec{Rules: []*rules.NamespaceRuleBodyTenant{{NamespaceSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"profile": "pdb"}}, NamespaceRuleBodyNamespace: body}}}}
				tnt.Status.Owners = rbac.OwnerStatusListSpec{{Kind: rbac.ServiceAccountOwner, Name: users.ServiceAccountUsername(name, "owner")}}
				objects = append(objects, tnt)
				for _, selected := range []bool{true, false} {
					namespace := fmt.Sprintf("%s-%v", name, selected)
					objects = append(objects, overlapNamespace(name, namespace, selected), overlapPDB(namespace, "one", &metav1.LabelSelector{}), overlapPDB(namespace, "two", &metav1.LabelSelector{}))
					if statusPresent {
						rs := &capsulev1beta2.RuleStatus{ObjectMeta: metav1.ObjectMeta{Name: meta.NameForManagedRuleStatus(), Namespace: namespace}}
						if selected {
							rs.Status.Rules = []*rules.NamespaceRuleBodyNamespace{body}
						}
						objects = append(objects, rs)
					}
				}
			}
			cl := &typeAdmissionClient{Client: fake.NewClientBuilder().WithScheme(scheme).WithObjects(objects...).Build()}
			cfg := configuration.NewCapsuleConfiguration(t.Context(), cl, cl, nil, "capsule")
			handlers := Register(nil, nil, cfg, nil, nil).GetHandlers()
			for _, name := range []string{"a", "b"} {
				for _, selected := range []bool{true, false} {
					for _, owner := range []bool{true, false} {
						namespace := fmt.Sprintf("%s-%v", name, selected)
						object := overlapObject("pod", "app", namespace, map[string]string{"app": "web"})
						req, _, _ := overlapRequest(t, object, nil)
						req.UserInfo = users.ServiceAccountUserInfo(name, "owner")
						if !owner {
							req.UserInfo = users.ServiceAccountUserInfo(name, "other")
						}
						for _, operation := range []admissionv1.Operation{admissionv1.Create, admissionv1.Update} {
							req.Operation = operation
							req.OldObject.Raw = []byte(fmt.Sprintf(`{"apiVersion":"v1","kind":"Pod","metadata":{"name":"app","namespace":%q}}`, namespace))
							cl.gets.Store(0)
							cl.lists.Store(0)
							reader := webhookutils.NewRequestCachingReader(cl)
							var response *admission.Response
							for _, h := range handlers {
								handler := h.OnCreate(cl, reader, admission.NewDecoder(scheme), testEventRecorder{})
								if operation == admissionv1.Update {
									handler = h.OnUpdate(cl, reader, admission.NewDecoder(scheme), testEventRecorder{})
								}
								response = handler(t.Context(), req)
								if response != nil {
									break
								}
							}
							denied := name == "a" && selected && owner
							if denied {
								require.NotNil(t, response)
								require.Contains(t, response.Result.Message, "PDB overlap")
							} else {
								require.Nil(t, response)
							}
							expectedGets := int64(3)
							if !statusPresent {
								// Tenant-rule fallback reads namespace labels through the cached
								// client to resolve the selector; this is existing pipeline work.
								expectedGets++
							}
							require.Equal(t, expectedGets, cl.gets.Load(), "reuse tenant/ruleset resolution")
							if denied {
								require.Equal(t, int64(1), cl.lists.Load())
							} else {
								require.Zero(t, cl.lists.Load())
							}
						}
					}
				}
			}
		})
	}
}

func BenchmarkDisruptionBudgetAdmission(b *testing.B) {
	for _, tenants := range []int{1, 8} {
		for _, count := range []int{2, 32} {
			for _, mode := range []string{"skip", "allow", "deny", "pdb"} {
				b.Run(fmt.Sprintf("tenants=%d/pdbs=%d/%s", tenants, count, mode), func(b *testing.B) {
					scheme := overlapScheme(b)
					var objects []client.Object
					requests := make([]admission.Request, tenants)
					for i := range tenants {
						ns := fmt.Sprintf("tenant-%d", i)
						tnt := &capsulev1beta2.Tenant{ObjectMeta: metav1.ObjectMeta{Name: ns, UID: types.UID(ns)}}
						rs := &capsulev1beta2.RuleStatus{ObjectMeta: metav1.ObjectMeta{Name: meta.NameForManagedRuleStatus(), Namespace: ns}}
						if mode != "skip" {
							rs.Status.Rules = []*rules.NamespaceRuleBodyNamespace{overlapRule(rules.ActionTypeAllow, false, rules.ValidatePod, rules.ValidateDeployment)}
						}
						objects = append(objects, tnt, overlapNamespace(ns, ns, true), rs)
						for j := range count {
							app := fmt.Sprintf("app-%d", j)
							if j == 0 || (j == 1 && mode == "deny") {
								app = "web"
							}
							objects = append(objects, overlapPDB(ns, fmt.Sprintf("budget-%03d", j), &metav1.LabelSelector{MatchLabels: map[string]string{"app": app}}))
						}
						requests[i], _, _ = overlapRequest(b, overlapObject("pod", "web", ns, map[string]string{"app": "web"}), nil)
						if mode == "pdb" {
							objects = append(objects, overlapObject("deployment", "web", ns, map[string]string{"app": "web"}))
							requests[i], _, _ = overlapRequest(b, overlapPDB(ns, "new", &metav1.LabelSelector{MatchLabels: map[string]string{"app": "web"}}), nil)
						}
					}
					cl := &typeAdmissionClient{Client: fake.NewClientBuilder().WithScheme(scheme).WithObjects(objects...).Build()}
					chain := Register(nil, cache.NewLabelSelectorCache(), nil, nil, nil).GetHandlers()
					decoder := admission.NewDecoder(scheme)
					run := func(req admission.Request) {
						reader := webhookutils.NewRequestCachingReader(cl)
						var response *admission.Response
						for _, h := range chain {
							response = h.OnCreate(cl, reader, decoder, testEventRecorder{})(b.Context(), req)
							if response != nil {
								break
							}
						}
						denied := mode == "deny" || mode == "pdb"
						if (response != nil) != denied {
							b.Fatalf("unexpected response %v", response)
						}
						if response != nil && response.Allowed {
							b.Fatal("unexpected allow")
						}
					}
					for _, req := range requests {
						run(req)
					}
					cl.gets.Store(0)
					cl.lists.Store(0)
					b.ReportAllocs()
					b.ResetTimer()
					for i := 0; b.Loop(); i++ {
						run(requests[i%tenants])
					}
					b.ReportMetric(float64(cl.gets.Load())/float64(b.N), "GET/op")
					b.ReportMetric(float64(cl.lists.Load())/float64(b.N), "LIST/op")
				})
			}
		}
	}
}

func TestDisruptionBudgetWritesThroughAdmissionChain(t *testing.T) {
	scheme := overlapScheme(t)
	tnt := &capsulev1beta2.Tenant{ObjectMeta: metav1.ObjectMeta{Name: "a", UID: "a"}}
	ns := overlapNamespace("a", "a", true)
	rs := &capsulev1beta2.RuleStatus{ObjectMeta: metav1.ObjectMeta{Name: meta.NameForManagedRuleStatus(), Namespace: "a"}}
	rs.Status.Rules = []*rules.NamespaceRuleBodyNamespace{overlapRule(rules.ActionTypeAllow, false, rules.ValidateDeployment)}
	existing := overlapObject("deployment", "web", "a", map[string]string{"app": "web"})
	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(tnt, ns, rs, existing, overlapPDB("a", "first", &metav1.LabelSelector{})).Build()
	handlers := Register(nil, nil, nil, nil, nil).GetHandlers()
	for _, operation := range []admissionv1.Operation{admissionv1.Create, admissionv1.Update} {
		object := overlapPDB("a", "second", &metav1.LabelSelector{})
		req, _, _ := overlapRequest(t, object, nil)
		if operation == admissionv1.Update {
			req, _, _ = overlapRequest(t, object, overlapPDB("a", "second", nil))
		}
		reader := webhookutils.NewRequestCachingReader(cl)
		var response *admission.Response
		for _, h := range handlers {
			handle := h.OnCreate(cl, reader, admission.NewDecoder(scheme), testEventRecorder{})
			if operation == admissionv1.Update {
				handle = h.OnUpdate(cl, reader, admission.NewDecoder(scheme), testEventRecorder{})
			}
			response = handle(t.Context(), req)
			if response != nil {
				break
			}
		}
		require.NotNil(t, response)
		require.Contains(t, response.Result.Message, "PDB overlap")
		require.Contains(t, response.Result.Message, "Deployment/web")
		require.Contains(t, response.Result.Message, "first, second")
	}
}

func TestDisruptionBudgetPodStatus(t *testing.T) {
	scheme := overlapScheme(t)
	tnt := &capsulev1beta2.Tenant{ObjectMeta: metav1.ObjectMeta{Name: "a", UID: "a"}}
	ns := overlapNamespace("a", "a", true)
	rs := &capsulev1beta2.RuleStatus{ObjectMeta: metav1.ObjectMeta{Name: meta.NameForManagedRuleStatus(), Namespace: "a"}}
	rs.Status.Rules = []*rules.NamespaceRuleBodyNamespace{overlapRule(rules.ActionTypeAllow, false)}
	selector := &metav1.LabelSelector{MatchLabels: map[string]string{"app": "web"}}
	cl := &typeAdmissionClient{Client: fake.NewClientBuilder().WithScheme(scheme).WithObjects(tnt, ns, rs, overlapPDB("a", "one", selector), overlapPDB("a", "two", selector)).Build()}
	chain := Register(nil, nil, nil, nil, nil).GetHandlers()
	for _, tc := range []struct {
		name, subresource, oldApp, newApp string
		denied                            bool
		gets, lists                       int64
	}{
		{"changed status labels", "status", "worker", "web", true, 3, 1},
		{"unchanged status labels", "status", "web", "web", false, 0, 0},
		{"repair status labels", "status", "web", "worker", false, 3, 1},
		{"scale", "scale", "worker", "web", false, 0, 0},
		{"ephemeral containers", "ephemeralcontainers", "worker", "web", false, 0, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			object := overlapObject("pod", "app", "a", map[string]string{"app": tc.newApp})
			old := overlapObject("pod", "app", "a", map[string]string{"app": tc.oldApp})
			req, _, _ := overlapRequest(t, object, old)
			req.SubResource = tc.subresource
			cl.gets.Store(0)
			cl.lists.Store(0)
			var response *admission.Response
			reader := webhookutils.NewRequestCachingReader(cl)
			for _, h := range chain {
				response = h.OnUpdate(cl, reader, admission.NewDecoder(scheme), testEventRecorder{})(t.Context(), req)
				if response != nil {
					break
				}
			}
			if tc.denied {
				require.NotNil(t, response)
				require.Contains(t, response.Result.Message, "PDB overlap")
			} else {
				require.Nil(t, response)
			}
			require.Equal(t, tc.gets, cl.gets.Load())
			require.Equal(t, tc.lists, cl.lists.Load())
		})
	}
}

func BenchmarkDisruptionBudgetUnchangedStatus(b *testing.B) {
	for _, mode := range []string{"baseline-route", "current"} {
		b.Run(mode, func(b *testing.B) {
			object := overlapObject("pod", "app", "a", map[string]string{"app": "web"})
			req, _, _ := overlapRequest(b, object, object)
			req.SubResource = "status"
			chain := Register(nil, nil, nil, nil, nil).GetHandlers()
			if mode == "baseline-route" {
				// Original routing predicate before status label checks were added.
				chain[0].(*matchingHandler).predicate = matchesGenericMetadataRequest
			}
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				for _, h := range chain {
					// Nil dependencies prove this path performs no lookups.
					if response := h.OnUpdate(nil, nil, nil, nil)(b.Context(), req); response != nil {
						b.Fatal(response)
					}
				}
			}
			b.ReportMetric(0, "GET/op")
			b.ReportMetric(0, "LIST/op")
		})
	}
}

func BenchmarkDisruptionBudgetRuleCount(b *testing.B) {
	for _, count := range []int{1, 20, 100} {
		b.Run(fmt.Sprintf("rules=%d", count), func(b *testing.B) {
			scheme := overlapScheme(b)
			cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(overlapPDB("a", "one", &metav1.LabelSelector{}), overlapPDB("a", "two", &metav1.LabelSelector{})).Build()
			h := &disruptionBudgetRules{selectors: cache.NewLabelSelectorCache()}
			bodies := make([]*rules.NamespaceRuleBodyNamespace, count)
			for i := range bodies {
				bodies[i] = overlapRule(rules.ActionTypeAllow, false)
			}
			req, obj, _ := overlapRequest(b, overlapObject("pod", "app", "a", nil), nil)
			handle := h.OnCreate(cl, cl, obj, admission.NewDecoder(scheme), testEventRecorder{}, &capsulev1beta2.Tenant{}, bodies)
			if response := handle(b.Context(), req); response == nil || response.Allowed {
				b.Fatal(response)
			}
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				if response := handle(b.Context(), req); response == nil || response.Allowed {
					b.Fatal(response)
				}
			}
			b.ReportMetric(1, "LIST/op")
		})
	}
}
