// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package validation

import (
	"context"
	"encoding/json"
	"fmt"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
	admissionv1 "k8s.io/api/admission/v1"
	authenticationv1 "k8s.io/api/authentication/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/internal/cache"
	podvalidation "github.com/projectcapsule/capsule/internal/webhook/rules/pods/validation"
	webhookutils "github.com/projectcapsule/capsule/internal/webhook/utils"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/api/rules"
	apiruntime "github.com/projectcapsule/capsule/pkg/api/runtime"
	"github.com/projectcapsule/capsule/pkg/tenant"
)

type typeAdmissionClient struct {
	client.Client
	gets, lists atomic.Int64
	failTenant  bool
}

func TestTemplateAdmissionSharesRequestReads(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))
	require.NoError(t, capsulev1beta2.AddToScheme(scheme))
	var objects []client.Object
	for _, name := range []string{"a", "b"} {
		tnt := &capsulev1beta2.Tenant{ObjectMeta: metav1.ObjectMeta{Name: name, UID: types.UID(name)}}
		ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: name, OwnerReferences: []metav1.OwnerReference{{APIVersion: capsulev1beta2.GroupVersion.String(), Kind: "Tenant", Name: name, UID: tnt.UID}}}}
		rs := &capsulev1beta2.RuleStatus{ObjectMeta: metav1.ObjectMeta{Name: meta.NameForManagedRuleStatus(), Namespace: name}}
		rs.Status.Rules = []*rules.NamespaceRuleBodyNamespace{{Enforce: &rules.NamespaceRuleEnforceBody{Workloads: rules.NamespaceRuleEnforceWorkloadsBody{Targets: []rules.WorkloadValidationTarget{rules.ValidateDeployment}, Schedulers: []apiruntime.ExpressionMatch{{Exact: []string{name}}}}}}}
		objects = append(objects, tnt, ns, rs)
	}
	cl := &typeAdmissionClient{Client: fake.NewClientBuilder().WithScheme(scheme).WithObjects(objects...).Build()}
	chain := Register(nil, nil, nil, nil, podvalidation.TemplateRules(nil, nil, nil)).GetHandlers()
	for _, namespace := range []string{"a", "b"} {
		cl.gets.Store(0)
		req := requestWithKind("apps", "Deployment")
		req.Namespace = namespace
		req.Object.Raw = []byte(fmt.Sprintf(`{"apiVersion":"apps/v1","kind":"Deployment","metadata":{"name":"app","namespace":%q},"spec":{"template":{"spec":{"schedulerName":"a","containers":[{"name":"app","image":"example.com/app:v1"}]}}}}`, namespace))
		reader := webhookutils.NewRequestCachingReader(cl)
		var response *admission.Response
		for _, handler := range chain {
			response = handler.OnCreate(cl, reader, admission.NewDecoder(scheme), testEventRecorder{})(t.Context(), req)
			if response != nil {
				break
			}
		}
		if namespace == "a" {
			require.NotNil(t, response)
			require.Contains(t, response.Result.Message, "scheduler")
		} else {
			require.Nil(t, response)
		}
		require.Equal(t, int64(3), cl.gets.Load(), "metadata and template handlers must share the three request reads")
		require.Zero(t, cl.lists.Load())
		cl.gets.Store(0)
		req.OldObject = req.Object
		req.Operation = admissionv1.Update
		reader = webhookutils.NewRequestCachingReader(cl)
		for _, handler := range chain {
			response = handler.OnUpdate(cl, reader, admission.NewDecoder(scheme), testEventRecorder{})(t.Context(), req)
			if response != nil {
				break
			}
		}
		if namespace == "a" {
			require.NotNil(t, response)
			require.Contains(t, response.Result.Message, "scheduler")
		} else {
			require.Nil(t, response)
		}
		require.Equal(t, int64(3), cl.gets.Load())
	}
	cl.gets.Store(0)
	for _, kind := range []string{"Service", "ConfigMap", "Deployment"} {
		req := requestWithKind("example.com", kind)
		req.Namespace = "a"
		h := &templateBridge{next: podvalidation.TemplateRules(nil, nil, nil)}
		require.Nil(t, h.OnCreate(cl, cl, nil, admission.NewDecoder(scheme), nil, nil, nil)(t.Context(), req))
		req.Kind.Group = "apps"
		req.SubResource = "status"
		require.Nil(t, h.OnUpdate(cl, cl, nil, nil, admission.NewDecoder(scheme), nil, nil, nil)(t.Context(), req))
	}
	require.Zero(t, cl.gets.Load(), "excluded requests skip resolution")
	bridge := &templateBridge{next: podvalidation.TemplateRules(nil, nil, nil)}
	req := requestWithKind("apps", "Deployment")
	for _, body := range []*rules.NamespaceRuleBodyNamespace{nil,
		{Enforce: typePolicy(rules.ActionTypeDeny, rules.ValidateDeployment)},
		{Enforce: &rules.NamespaceRuleEnforceBody{Workloads: rules.NamespaceRuleEnforceWorkloadsBody{Targets: []rules.WorkloadValidationTarget{rules.ValidateJob}, Schedulers: []apiruntime.ExpressionMatch{{Exact: []string{"batch"}}}}}},
	} {
		// A nil decoder catches accidental full-object decoding before the skip.
		require.Nil(t, bridge.OnCreate(nil, nil, nil, nil, nil, nil, []*rules.NamespaceRuleBodyNamespace{body})(t.Context(), req))
		require.Nil(t, bridge.OnUpdate(nil, nil, nil, nil, nil, nil, nil, []*rules.NamespaceRuleBodyNamespace{body})(t.Context(), req))
	}
}

func BenchmarkTemplateHandlerChain(b *testing.B) {
	for _, tenants := range []int{1, 8} {
		for _, count := range []int{0, 20} {
			for _, enabled := range []bool{false, true} {
				b.Run(fmt.Sprintf("tenants=%d/rules=%d/templates=%v", tenants, count, enabled), func(b *testing.B) {
					scheme := runtime.NewScheme()
					require.NoError(b, corev1.AddToScheme(scheme))
					require.NoError(b, capsulev1beta2.AddToScheme(scheme))
					objects := make([]client.Object, 0, tenants*3)
					requests := make([]admission.Request, tenants)
					for i := range tenants {
						name := fmt.Sprintf("tenant-%d", i)
						tnt := &capsulev1beta2.Tenant{ObjectMeta: metav1.ObjectMeta{Name: name, UID: types.UID(name)}}
						ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: name, OwnerReferences: []metav1.OwnerReference{{APIVersion: capsulev1beta2.GroupVersion.String(), Kind: "Tenant", Name: name, UID: tnt.UID}}}}
						rs := &capsulev1beta2.RuleStatus{ObjectMeta: metav1.ObjectMeta{Name: meta.NameForManagedRuleStatus(), Namespace: name}}
						for range count {
							rs.Status.Rules = append(rs.Status.Rules, &rules.NamespaceRuleBodyNamespace{Enforce: &rules.NamespaceRuleEnforceBody{Workloads: rules.NamespaceRuleEnforceWorkloadsBody{Targets: []rules.WorkloadValidationTarget{rules.ValidateDeployment}, Schedulers: []apiruntime.ExpressionMatch{{Exact: []string{"blocked"}}}}}})
						}
						objects = append(objects, tnt, ns, rs)
						requests[i] = requestWithKind("apps", "Deployment")
						requests[i].Namespace = name
						requests[i].Object.Raw = []byte(fmt.Sprintf(`{"apiVersion":"apps/v1","kind":"Deployment","metadata":{"name":"app","namespace":%q},"spec":{"template":{"spec":{"schedulerName":"default-scheduler","containers":[{"name":"app","image":"example.com/app:v1"}]}}}}`, name))
					}
					cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objects...).Build()
					chain := Register(nil, nil, nil, nil, nil).GetHandlers()
					if enabled {
						chain = Register(nil, nil, nil, nil, podvalidation.TemplateRules(nil, nil, nil)).GetHandlers()
					}
					decoder := admission.NewDecoder(scheme)
					b.ReportAllocs()
					b.ResetTimer()
					for i := 0; b.Loop(); i++ {
						reader := webhookutils.NewRequestCachingReader(cl)
						for _, handler := range chain {
							if got := handler.OnCreate(cl, reader, decoder, testEventRecorder{})(b.Context(), requests[i%tenants]); got != nil {
								b.Fatalf("unexpected response: %v", got)
							}
						}
					}
				})
			}
		}
	}
}

func (c *typeAdmissionClient) Get(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
	c.gets.Add(1)
	if _, ok := obj.(*capsulev1beta2.Tenant); ok && c.failTenant {
		return fmt.Errorf("tenant read unavailable")
	}
	return c.Client.Get(ctx, key, obj, opts...)
}

func (c *typeAdmissionClient) List(ctx context.Context, obj client.ObjectList, opts ...client.ListOption) error {
	c.lists.Add(1)
	return c.Client.List(ctx, obj, opts...)
}

func TestWorkloadTypeAdmissionProfilesAndReads(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))
	require.NoError(t, capsulev1beta2.AddToScheme(scheme))
	compiler, err := cache.NewCELCache()
	require.NoError(t, err)
	for _, statusPresent := range []bool{false, true} {
		t.Run(fmt.Sprintf("RuleStatus=%v", statusPresent), func(t *testing.T) {
			var objects []client.Object
			for _, name := range []string{"a", "b"} {
				typ := rules.ValidateDaemonSet
				if name == "b" {
					typ = rules.ValidateJob
				}
				body := &rules.NamespaceRuleBodyNamespace{Audience: []rules.Audience{{Kind: rules.AudienceKindUser, Name: name}}, Enforce: typePolicy(rules.ActionTypeDeny, typ)}
				tnt := &capsulev1beta2.Tenant{ObjectMeta: metav1.ObjectMeta{Name: name, UID: types.UID(name)}, Spec: capsulev1beta2.TenantSpec{Rules: []*rules.NamespaceRuleBodyTenant{{NamespaceSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"profile": "selected"}}, NamespaceRuleBodyNamespace: body}}}}
				objects = append(objects, tnt)
				for _, profile := range []string{"selected", "other"} {
					ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: name + "-" + profile, Labels: map[string]string{meta.TenantLabel: name, "profile": profile}, OwnerReferences: []metav1.OwnerReference{{APIVersion: capsulev1beta2.GroupVersion.String(), Kind: "Tenant", Name: name, UID: tnt.UID}}}}
					objects = append(objects, ns)
					if statusPresent {
						projected, err := tenant.BuildNamespaceRuleBodyStatus(scheme, ns, tnt)
						require.NoError(t, err)
						rs := &capsulev1beta2.RuleStatus{ObjectMeta: metav1.ObjectMeta{Name: meta.NameForManagedRuleStatus(), Namespace: ns.Name}}
						rs.Status.Rules = projected
						objects = append(objects, rs)
					}
				}
			}
			cl := &typeAdmissionClient{Client: fake.NewClientBuilder().WithScheme(scheme).WithObjects(objects...).Build()}
			for _, tc := range []struct {
				namespace, user, group, kind string
				denied                       bool
			}{
				{"a-selected", "a", "apps", "DaemonSet", true},
				{"a-other", "a", "apps", "DaemonSet", false},
				{"a-selected", "b", "apps", "DaemonSet", false},
				{"b-selected", "b", "apps", "DaemonSet", false},
				{"b-selected", "b", "batch", "Job", true},
			} {
				for _, operation := range []admissionv1.Operation{admissionv1.Create, admissionv1.Update} {
					t.Run(tc.namespace+"/"+tc.user+"/"+tc.kind+"/"+string(operation), func(t *testing.T) {
						cl.gets.Store(0)
						cl.lists.Store(0)
						req := requestWithKind(tc.group, tc.kind)
						req.Namespace = tc.namespace
						req.Operation = operation
						req.UserInfo = authenticationv1.UserInfo{Username: tc.user}
						obj := &metav1.PartialObjectMetadata{TypeMeta: metav1.TypeMeta{APIVersion: tc.group + "/v1", Kind: tc.kind}, ObjectMeta: metav1.ObjectMeta{Name: "app", Namespace: tc.namespace}}
						req.Object.Raw, err = json.Marshal(obj)
						require.NoError(t, err)
						req.OldObject = req.Object
						spy := &requestSpyHandler{}
						chain := Register(nil, nil, nil, compiler, nil, spy).GetHandlers()
						var response *admission.Response
						for _, handler := range chain {
							if operation == admissionv1.Create {
								response = handler.OnCreate(cl, cl, admission.NewDecoder(scheme), testEventRecorder{})(t.Context(), req)
							} else {
								response = handler.OnUpdate(cl, cl, admission.NewDecoder(scheme), testEventRecorder{})(t.Context(), req)
							}
							if response != nil {
								break
							}
						}
						if tc.denied {
							require.NotNil(t, response)
							require.False(t, response.Allowed)
							require.Contains(t, response.Result.Message, "workload type")
							require.Zero(t, spy.calls)
						} else {
							require.Nil(t, response)
							require.Equal(t, 1, spy.calls, "allows must continue the chain")
						}
						wantReads := int64(4)
						if statusPresent {
							wantReads = 3
						}
						require.Equal(t, wantReads, cl.gets.Load())
						require.Zero(t, cl.lists.Load())
					})
				}
			}
			for _, subresource := range []string{"status", "scale"} {
				cl.gets.Store(0)
				req := requestWithKind("apps", "DaemonSet")
				req.Namespace = "a-selected"
				req.SubResource = subresource
				h := Register(nil, nil, nil, compiler, nil).GetHandlers()[0]
				require.Nil(t, h.OnUpdate(cl, cl, admission.NewDecoder(scheme), nil)(t.Context(), req))
				require.Zero(t, cl.gets.Load())
			}
			cl.failTenant = true
			req := requestWithKind("apps", "DaemonSet")
			req.Namespace = "a-selected"
			response := Register(nil, nil, nil, compiler, nil).GetHandlers()[0].OnCreate(cl, cl, admission.NewDecoder(scheme), nil)(t.Context(), req)
			require.NotNil(t, response)
			require.False(t, response.Allowed)
			require.Contains(t, response.Result.Message, "tenant read unavailable")
		})
	}
}
