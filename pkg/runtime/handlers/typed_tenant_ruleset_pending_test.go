// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package handlers_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	mockclient "github.com/projectcapsule/capsule/internal/mocks/client"
	servicevalidation "github.com/projectcapsule/capsule/internal/webhook/rules/services/validation"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/api/rules"
	"github.com/projectcapsule/capsule/pkg/runtime/events"
	"github.com/projectcapsule/capsule/pkg/runtime/handlers"
	webhookutils "github.com/projectcapsule/capsule/pkg/runtime/webhook/utils"
)

func pendingRulesFixture(tb testing.TB, state string, tenants, count int) (client.WithWatch, *handlers.TypedTenantWithRulesetHandler[*corev1.Service]) {
	tb.Helper()
	scheme := runtime.NewScheme()
	require.NoError(tb, corev1.AddToScheme(scheme))
	require.NoError(tb, capsulev1beta2.AddToScheme(scheme))
	body := &rules.NamespaceRuleBodyNamespace{Enforce: &rules.NamespaceRuleEnforceBody{
		Action: rules.ActionTypeDeny, Services: rules.NamespaceRuleEnforceServicesBody{Types: []rules.ServiceType{rules.ServiceTypeNodePort}},
	}}
	tnt := &capsulev1beta2.Tenant{Name: "tenant-a", UID: "tenant-a-uid"}
	for range count {
		tnt.Spec.Rules = append(tnt.Spec.Rules, &rules.NamespaceRuleBodyTenant{
			NamespaceSelector:          &metav1.LabelSelector{MatchLabels: map[string]string{"profile": "guarded"}},
			NamespaceRuleBodyNamespace: body.DeepCopy(),
		})
	}
	objects := []client.Object{tnt}
	for i := 1; i < tenants; i++ {
		objects = append(objects, &capsulev1beta2.Tenant{Name: fmt.Sprintf("tenant-%d", i), UID: types.UID(fmt.Sprintf("tenant-%d-uid", i))})
	}
	for _, name := range []string{"selected", "plain", "foreign"} {
		ns := &corev1.Namespace{Name: name, UID: types.UID(name), Labels: map[string]string{"profile": "guarded"},
			OwnerReferences: []metav1.OwnerReference{{APIVersion: capsulev1beta2.GroupVersion.String(), Kind: "Tenant", Name: tnt.Name, UID: tnt.UID}},
		}
		if name == "plain" {
			ns.Labels["profile"] = "plain"
		}
		if name == "foreign" {
			ns.OwnerReferences[0].Name, ns.OwnerReferences[0].UID = "tenant-1", "tenant-1-uid"
		}
		objects = append(objects, ns)
		if state == "missing" {
			continue
		}
		rs := &capsulev1beta2.RuleStatus{Name: meta.NameForManagedRuleStatus(), Namespace: name, Generation: 2}
		if name == "selected" {
			for range count {
				rs.Spec = append(rs.Spec, body.DeepCopy())
			}
		}
		switch state {
		case "current":
			rs.Status.ObservedGeneration, rs.Status.Rules = rs.Generation, rs.Spec
		case "stale":
			rs.Status.ObservedGeneration = 1
		}
		objects = append(objects, rs)
	}
	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objects...).Build()
	handler := &handlers.TypedTenantWithRulesetHandler[*corev1.Service]{
		Factory:  func() *corev1.Service { return &corev1.Service{} },
		Handlers: []handlers.TypedHandlerWithTenantWithRuleset[*corev1.Service]{servicevalidation.ServiceRules(nil, nil)},
	}
	return cl, handler
}

func pendingRulesRequest(tb testing.TB, namespace string, operation admissionv1.Operation, serviceType corev1.ServiceType) admission.Request {
	tb.Helper()
	raw, err := json.Marshal(&corev1.Service{Name: "service", Namespace: namespace, Spec: corev1.ServiceSpec{Type: serviceType, Ports: []corev1.ServicePort{{Port: 80}}}})
	require.NoError(tb, err)
	return admission.Request{Operation: operation, Namespace: namespace, Name: "service",
		Kind: metav1.GroupVersionKind{Version: "v1", Kind: "Service"}, Object: runtime.RawExtension{Raw: raw}, OldObject: runtime.RawExtension{Raw: raw},
	}
}

func TestRulesetAdmissionEnforcesPendingProfiles(t *testing.T) {
	for _, state := range []string{"current", "pending", "stale", "missing"} {
		for _, operation := range []admissionv1.Operation{admissionv1.Create, admissionv1.Update} {
			t.Run(state+"/"+string(operation), func(t *testing.T) {
				cl, handler := pendingRulesFixture(t, state, 2, 2)
				var reads int
				reader := interceptor.NewClient(cl, interceptor.Funcs{Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
					if _, ok := obj.(*corev1.Namespace); ok {
						reads++
					}
					return c.Get(ctx, key, obj, opts...)
				}})
				fn := handler.OnCreate(reader, reader, admission.NewDecoder(cl.Scheme()), events.NewDiscardRecorder())
				if operation == admissionv1.Update {
					fn = handler.OnUpdate(reader, reader, admission.NewDecoder(cl.Scheme()), events.NewDiscardRecorder())
				}
				for _, tc := range []struct {
					namespace string
					kind      corev1.ServiceType
					deny      bool
				}{{"selected", corev1.ServiceTypeNodePort, true}, {"selected", corev1.ServiceTypeClusterIP, false}, {"plain", corev1.ServiceTypeNodePort, false}, {"foreign", corev1.ServiceTypeNodePort, false}} {
					reads = 0
					response := fn(t.Context(), pendingRulesRequest(t, tc.namespace, operation, tc.kind))
					if tc.deny {
						require.NotNil(t, response)
						require.False(t, response.Allowed)
						require.Contains(t, response.Result.Message, "denied by namespace rule")
					} else {
						require.Nil(t, response, "allowed requests must continue the handler chain")
					}
					want := 2 // Tenant resolution plus the existing fallback read.
					if state == "current" {
						want = 1
					}
					require.Equal(t, want, reads)
				}
			})
		}
	}
}

func TestPendingRulesetReadErrorsDoNotAllowRequests(t *testing.T) {
	for _, target := range []string{"ruleset", "fallback"} {
		t.Run(target, func(t *testing.T) {
			cl, handler := pendingRulesFixture(t, "pending", 2, 1)
			readErr := errors.New("profile reader unavailable")
			var namespaces, reads atomic.Int32
			reader := interceptor.NewClient(cl, interceptor.Funcs{Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
				reads.Add(1)
				if _, ok := obj.(*capsulev1beta2.RuleStatus); ok && target == "ruleset" {
					return readErr
				}
				if _, ok := obj.(*corev1.Namespace); ok && namespaces.Add(1) == 2 && target == "fallback" {
					return readErr
				}
				return c.Get(ctx, key, obj, opts...)
			}})
			fn := handler.OnCreate(reader, reader, admission.NewDecoder(cl.Scheme()), events.NewDiscardRecorder())
			req := pendingRulesRequest(t, "selected", admissionv1.Create, corev1.ServiceTypeNodePort)
			response := fn(t.Context(), req)
			require.NotNil(t, response)
			require.False(t, response.Allowed)
			require.Contains(t, response.Result.Message, readErr.Error())
			reads.Store(0)
			req.Namespace = ""
			require.Nil(t, fn(t.Context(), req))
			require.Zero(t, reads.Load(), "irrelevant requests must skip profile reads")
		})
	}
}

func TestPendingRulesetReusesAuthoritativeNamespaceSnapshot(t *testing.T) {
	cl, handler := pendingRulesFixture(t, "pending", 2, 1)
	var namespaces atomic.Int32
	reader := interceptor.NewClient(cl, interceptor.Funcs{Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
		if _, ok := obj.(*corev1.Namespace); ok {
			namespaces.Add(1)
		}
		return c.Get(ctx, key, obj, opts...)
	}})
	// Simulate a manager cache which has not observed the profile label yet.
	stale := &corev1.Namespace{}
	require.NoError(t, cl.Get(t.Context(), client.ObjectKey{Name: "selected"}, stale))
	stale.Labels["profile"] = "plain"
	cached := fake.NewClientBuilder().WithScheme(cl.Scheme()).WithObjects(stale).Build()
	for _, operation := range []admissionv1.Operation{admissionv1.Create, admissionv1.Update} {
		namespaces.Store(0)
		requestReader := webhookutils.NewRequestCachingReader(reader)
		fn := handler.OnCreate(cached, requestReader, admission.NewDecoder(cl.Scheme()), events.NewDiscardRecorder())
		if operation == admissionv1.Update {
			fn = handler.OnUpdate(cached, requestReader, admission.NewDecoder(cl.Scheme()), events.NewDiscardRecorder())
		}
		response := fn(t.Context(), pendingRulesRequest(t, "selected", operation, corev1.ServiceTypeNodePort))
		require.NotNil(t, response)
		require.False(t, response.Allowed)
		require.Contains(t, response.Result.Message, "denied by namespace rule")
		require.EqualValues(t, 1, namespaces.Load(), "fallback must reuse the authoritative Namespace read")
	}
}

func BenchmarkRulesetPublicationAdmission(b *testing.B) {
	for _, state := range []string{"current", "pending"} {
		for _, tenants := range []int{1, 16} {
			for _, count := range []int{1, 32} {
				for _, decision := range []string{"allow", "deny", "skip"} {
					b.Run(fmt.Sprintf("%s/tenants=%d/rules=%d/%s", state, tenants, count, decision), func(b *testing.B) {
						cl, handler := pendingRulesFixture(b, state, tenants, count)
						calls := &mockclient.CallCounter{}
						reader := interceptor.NewClient(cl, calls.Interceptors())
						decoder, recorder := admission.NewDecoder(cl.Scheme()), events.NewDiscardRecorder()
						req := pendingRulesRequest(b, "selected", admissionv1.Create, corev1.ServiceTypeClusterIP)
						if decision == "deny" {
							req = pendingRulesRequest(b, "selected", admissionv1.Create, corev1.ServiceTypeNodePort)
						} else if decision == "skip" {
							req.Namespace = ""
						}
						b.ReportAllocs()
						for b.Loop() {
							fn := handler.OnCreate(reader, webhookutils.NewRequestCachingReader(reader), decoder, recorder)
							response := fn(b.Context(), req)
							if decision == "deny" {
								if response == nil || response.Allowed {
									b.Fatal("pending profile allowed a forbidden Service")
								}
							} else if response != nil {
								b.Fatalf("unexpected admission response: %+v", response)
							}
						}
						calls.Report(b)
					})
				}
			}
		}
	}
}
