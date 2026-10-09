// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package validation

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/require"
	admissionv1 "k8s.io/api/admission/v1"
	authenticationv1 "k8s.io/api/authentication/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apiserver/pkg/cel/environment"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/internal/cache"
	genericvalidation "github.com/projectcapsule/capsule/internal/webhook/rules/generic/validation"
	webhookutils "github.com/projectcapsule/capsule/internal/webhook/utils"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/api/rules"
	apiruntime "github.com/projectcapsule/capsule/pkg/api/runtime"
	"github.com/projectcapsule/capsule/pkg/ruleengine"
	celruntime "github.com/projectcapsule/capsule/pkg/runtime/cel"
	"github.com/projectcapsule/capsule/pkg/runtime/events"
	"github.com/projectcapsule/capsule/pkg/runtime/handlers"
)

type countingClient struct {
	client.Client
	gets, lists atomic.Int64
	failTenant  bool
}

func (c *countingClient) Get(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
	c.gets.Add(1)
	if _, ok := obj.(*capsulev1beta2.Tenant); ok && c.failTenant {
		return fmt.Errorf("tenant unavailable")
	}
	return c.Client.Get(ctx, key, obj, opts...)
}

func (c *countingClient) List(ctx context.Context, obj client.ObjectList, opts ...client.ListOption) error {
	c.lists.Add(1)
	return c.Client.List(ctx, obj, opts...)
}

func admissionFixture(tb testing.TB, tenants int, status bool, bodies func(int) []*rules.NamespaceRuleBodyNamespace, ingress ...bool) (*countingClient, admission.Decoder, []admission.Request) {
	tb.Helper()
	scheme := runtime.NewScheme()
	require.NoError(tb, corev1.AddToScheme(scheme))
	require.NoError(tb, networkingv1.AddToScheme(scheme))
	require.NoError(tb, capsulev1beta2.AddToScheme(scheme))
	if len(ingress) > 0 && ingress[0] {
		originalBodies := bodies
		bodies = func(i int) []*rules.NamespaceRuleBodyNamespace {
			var result []*rules.NamespaceRuleBodyNamespace
			for _, body := range originalBodies(i) {
				copy := body.DeepCopy()
				if copy.Enforce != nil {
					_, converted := ingressFixtures(nil, []*rules.NamespaceRuleEnforceBody{copy.Enforce})
					copy.Enforce = converted[0]
				}
				result = append(result, copy)
			}
			return result
		}
	}
	var objects []client.Object
	var requests []admission.Request
	for i := range tenants {
		name := fmt.Sprintf("tenant-%d", i)
		tnt := &capsulev1beta2.Tenant{ObjectMeta: metav1.ObjectMeta{Name: name, UID: types.UID(name)}}
		for _, body := range bodies(i) {
			tnt.Spec.Rules = append(tnt.Spec.Rules, &rules.NamespaceRuleBodyTenant{NamespaceRuleBodyNamespace: body})
		}
		ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: name, OwnerReferences: []metav1.OwnerReference{{APIVersion: capsulev1beta2.GroupVersion.String(), Kind: "Tenant", Name: name, UID: tnt.UID}}}}
		objects = append(objects, tnt, ns)
		if status {
			rs := &capsulev1beta2.RuleStatus{ObjectMeta: metav1.ObjectMeta{Name: meta.NameForManagedRuleStatus(), Namespace: name}}
			rs.Status.Rules = bodies(i)
			objects = append(objects, rs)
		}
		obj := policy("10.20.0.0/16")
		if len(ingress) > 0 && ingress[0] {
			obj, _ = ingressFixtures(obj, nil)
		}
		obj.Name, obj.Namespace = "policy", name
		obj.APIVersion, obj.Kind = "networking.k8s.io/v1", "NetworkPolicy"
		raw, err := json.Marshal(obj)
		require.NoError(tb, err)
		requests = append(requests, admission.Request{AdmissionRequest: admissionv1.AdmissionRequest{
			Kind:      metav1.GroupVersionKind{Group: "networking.k8s.io", Version: "v1", Kind: "NetworkPolicy"},
			Namespace: name, Name: obj.Name, Operation: admissionv1.Create,
			Object: runtime.RawExtension{Raw: raw}, UserInfo: authenticationv1.UserInfo{Username: "alice"},
		}})
	}
	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objects...).WithStatusSubresource(&capsulev1beta2.RuleStatus{}).Build()
	return &countingClient{Client: cl}, admission.NewDecoder(scheme), requests
}

func runChain(ctx context.Context, cl *countingClient, decoder admission.Decoder, chain []handlers.Handler, req admission.Request) *admission.Response {
	reader := webhookutils.NewRequestCachingReader(cl)
	recorder := events.NewEventRecorder(nil, logr.Discard(), nil, nil)
	for _, handler := range chain {
		handle := handler.OnCreate(cl, reader, decoder, recorder)
		if req.Operation == admissionv1.Update {
			handle = handler.OnUpdate(cl, reader, decoder, recorder)
		}
		if response := handle(ctx, req); response != nil {
			return response
		}
	}
	return nil
}

func TestNetworkPolicyAdmissionProfilesReadsAndFallback(t *testing.T) {
	for _, ingress := range []bool{false, true} {
		direction := "egress"
		if ingress {
			direction = "ingress"
		}
		t.Run(direction, func(t *testing.T) {
			for _, status := range []bool{true, false} {
				t.Run(fmt.Sprintf("status=%v", status), func(t *testing.T) {
					cl, decoder, requests := admissionFixture(t, 2, status, func(i int) []*rules.NamespaceRuleBodyNamespace {
						cidr := "10.20.0.0/16"
						if i == 1 {
							cidr = "192.0.2.0/24"
						}
						return []*rules.NamespaceRuleBodyNamespace{{Enforce: cidrRule(rules.ActionTypeDeny, cidr)}}
					}, ingress)
					chain := genericvalidation.Register(nil, nil, nil, nil, nil, Handler(nil, nil)).GetHandlers()
					for i, req := range requests {
						for _, operation := range []admissionv1.Operation{admissionv1.Create, admissionv1.Update} {
							cl.gets.Store(0)
							cl.lists.Store(0)
							req.Operation = operation
							req.OldObject.Raw = []byte(`{"apiVersion":"networking.k8s.io/v1","kind":"NetworkPolicy","metadata":{"name":"policy"},"spec":{"podSelector":{},"policyTypes":["Egress"]}}`)
							response := runChain(t.Context(), cl, decoder, chain, req)
							if i == 0 {
								require.NotNil(t, response)
								require.Contains(t, response.Result.Message, "networkPolicy "+direction+" CIDR")
							} else {
								require.Nil(t, response)
							}
							wantGets := int64(3)
							if !status {
								// Each existing typed wrapper reconstructs missing status with
								// a Namespace read from the manager's cached client.
								wantGets += 2
							}
							require.Equal(t, wantGets, cl.gets.Load(), "direct request reads remain shared")
							require.Zero(t, cl.lists.Load())
						}
					}
					cl.failTenant = true
					response := runChain(t.Context(), cl, decoder, chain, requests[0])
					require.NotNil(t, response)
					require.False(t, response.Allowed)
					require.Contains(t, response.Result.Message, "tenant unavailable")
				})
			}
		})
	}
}

func TestNetworkPolicyAdmissionConditionsAndAudience(t *testing.T) {
	for _, ingress := range []bool{false, true} {
		direction := "egress"
		if ingress {
			direction = "ingress"
		}
		t.Run(direction, func(t *testing.T) {
			for _, tc := range []struct{ name, expression, audience, message string }{
				{"condition true", `object.spec.egress.size() > 0`, "alice", "networkPolicy " + direction + " CIDR"},
				{"condition false", "false", "alice", ""},
				{"other audience", "true", "bob", ""},
				{"condition error", `object.spec.missing == 'x'`, "alice", "conditions[0]"},
			} {
				t.Run(tc.name, func(t *testing.T) {
					compiler, err := cache.NewCELCache()
					require.NoError(t, err)
					cl, decoder, requests := admissionFixture(t, 1, true, func(int) []*rules.NamespaceRuleBodyNamespace {
						body := cidrRule(rules.ActionTypeDeny, "0.0.0.0/0")
						body.Conditions = []rules.AdmissionCondition{{Expression: strings.ReplaceAll(tc.expression, "egress", direction)}}
						return []*rules.NamespaceRuleBodyNamespace{{Enforce: body, Audience: []rules.Audience{{Kind: rules.AudienceKindUser, Name: tc.audience}}}}
					}, ingress)
					response := runChain(t.Context(), cl, decoder, []handlers.Handler{Handler(nil, compiler)}, requests[0])
					if tc.message == "" {
						require.Nil(t, response)
					} else {
						require.NotNil(t, response)
						require.Contains(t, response.Result.Message, tc.message)
					}
				})
			}
		})
	}
}

func TestNetworkPolicyAllowContinuesChain(t *testing.T) {
	for _, ingress := range []bool{false, true} {
		direction := "egress"
		if ingress {
			direction = "ingress"
		}
		t.Run(direction, func(t *testing.T) {
			cl, decoder, requests := admissionFixture(t, 1, true, func(int) []*rules.NamespaceRuleBodyNamespace {
				return []*rules.NamespaceRuleBodyNamespace{
					{Enforce: cidrRule(rules.ActionTypeAllow, "10.0.0.0/8")},
					{Enforce: &rules.NamespaceRuleEnforceBody{
						Action: rules.ActionTypeAllow,
						Metadata: []rules.MetadataRule{{
							VersionKinds: apiruntime.VersionKinds{APIGroups: []string{"networking.k8s.io/v1"}, Kinds: []string{"NetworkPolicy"}},
							Labels:       map[string]rules.MetadataValueRule{"required": {Required: true}},
						}},
					}},
				}
			}, ingress)
			chain := append([]handlers.Handler{Handler(nil, nil)}, genericvalidation.Register(nil, nil, nil, nil, nil).GetHandlers()...)
			response := runChain(t.Context(), cl, decoder, chain, requests[0])
			require.NotNil(t, response)
			require.Contains(t, response.Result.Message, "required")
		})
	}
}

func TestNetworkPolicyExcludedRequestsDoNotRead(t *testing.T) {
	cl, decoder, requests := admissionFixture(t, 1, true, func(int) []*rules.NamespaceRuleBodyNamespace { return nil })
	h := Handler(nil, nil)
	for _, tc := range []struct{ group, kind, subresource string }{{"", "ConfigMap", ""}, {"example.com", "NetworkPolicy", ""}, {"networking.k8s.io", "NetworkPolicy", "status"}} {
		req := requests[0]
		req.Kind.Group, req.Kind.Kind, req.SubResource = tc.group, tc.kind, tc.subresource
		require.Nil(t, h.OnCreate(cl, cl, decoder, nil)(t.Context(), req))
		require.Nil(t, h.OnUpdate(cl, cl, decoder, nil)(t.Context(), req))
	}
	require.Nil(t, h.OnDelete(cl, cl, decoder, nil)(t.Context(), requests[0]))
	require.Zero(t, cl.gets.Load())
	require.Zero(t, cl.lists.Load())
}

type countingConditions struct {
	ruleengine.ConditionCompiler
	calls int
}

func (c *countingConditions) GetOrCompileCondition(expression string, mode environment.Type) (*celruntime.CompiledExpression, error) {
	c.calls++
	return c.ConditionCompiler.GetOrCompileCondition(expression, mode)
}

func TestNetworkPolicySharedConditionGatesBothDirections(t *testing.T) {
	for _, expression := range []string{"true", "false"} {
		t.Run(expression, func(t *testing.T) {
			compiler, err := cache.NewCELCache()
			require.NoError(t, err)
			counted := &countingConditions{ConditionCompiler: compiler}
			cl, decoder, requests := admissionFixture(t, 1, true, func(int) []*rules.NamespaceRuleBodyNamespace {
				body := cidrRule(rules.ActionTypeDeny, "192.0.2.0/24")
				body.Network.Policies.Ingress = &rules.NetworkPolicyCIDRRule{CIDRs: []string{"10.0.0.0/8"}}
				body.Conditions = []rules.AdmissionCondition{{Expression: expression}}
				return []*rules.NamespaceRuleBodyNamespace{{Enforce: body}}
			})
			obj := &networkingv1.NetworkPolicy{}
			require.NoError(t, json.Unmarshal(requests[0].Object.Raw, obj))
			obj.Spec.PolicyTypes = append(obj.Spec.PolicyTypes, networkingv1.PolicyTypeIngress)
			obj.Spec.Ingress = []networkingv1.NetworkPolicyIngressRule{{From: obj.Spec.Egress[0].To}}
			requests[0].Object.Raw, err = json.Marshal(obj)
			require.NoError(t, err)
			for range 2 { // Cold and warm shared CEL cache, with fresh request evaluation.
				counted.calls = 0
				response := runChain(t.Context(), cl, decoder, []handlers.Handler{Handler(nil, counted)}, requests[0])
				require.Equal(t, 1, counted.calls, "one condition evaluation for the whole enforcement block")
				if expression == "false" {
					require.Nil(t, response)
				} else {
					require.NotNil(t, response)
					require.False(t, response.Allowed)
					require.Contains(t, response.Result.Message, "networkPolicy ingress CIDR")
				}
			}
		})
	}
}
