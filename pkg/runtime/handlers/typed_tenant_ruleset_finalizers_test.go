// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package handlers_test

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	admissionv1 "k8s.io/api/admission/v1"
	authenticationv1 "k8s.io/api/authentication/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	mockclient "github.com/projectcapsule/capsule/internal/mocks/client"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/api/rules"
	"github.com/projectcapsule/capsule/pkg/runtime/configuration"
	"github.com/projectcapsule/capsule/pkg/runtime/handlers"
)

func finalizerRulesRequest(tb testing.TB, old, obj *corev1.ConfigMap) admission.Request {
	tb.Helper()
	previous, err := json.Marshal(old)
	require.NoError(tb, err)
	raw, err := json.Marshal(obj)
	require.NoError(tb, err)
	return admission.Request{Operation: admissionv1.Update, Namespace: old.Namespace, Name: old.Name,
		Kind:      metav1.GroupVersionKind{Version: "v1", Kind: "ConfigMap"},
		UserInfo:  authenticationv1.UserInfo{Username: "system:serviceaccount:capsule-system:capsule"},
		OldObject: runtime.RawExtension{Raw: previous}, Object: runtime.RawExtension{Raw: raw},
	}
}

func finalizerRulesObject(size int) *corev1.ConfigMap {
	stamp := metav1.Now()
	obj := &corev1.ConfigMap{APIVersion: "v1", Kind: "ConfigMap", Name: "held", Namespace: "tenant-a",
		UID: "held-uid", ResourceVersion: "1", DeletionTimestamp: &stamp,
		Finalizers: []string{meta.ControllerFinalizer, "example.com/hold", meta.LegacyResourceFinalizer},
		Data:       map[string]string{"profile": "original"}, Labels: map[string]string{"profile": "selected"},
	}
	for i := range size {
		obj.Data[fmt.Sprintf("key-%d", i)] = strings.Repeat("content", 16)
	}
	return obj
}

func TestRulesetControllerFinalizerRemovalPreservesGuards(t *testing.T) {
	t.Setenv(configuration.EnvironmentControllerNamespace, "capsule-system")
	t.Setenv(configuration.EnvironmentServiceaccountName, "capsule")
	for _, tc := range []struct {
		name   string
		change func(*corev1.ConfigMap, *corev1.ConfigMap, *admission.Request)
		skip   bool
	}{
		{name: "cleanup", skip: true},
		{name: "existing duplicate lifecycle finalizers", skip: true, change: func(old, obj *corev1.ConfigMap, _ *admission.Request) {
			old.Finalizers = []string{meta.ControllerFinalizer, meta.ControllerFinalizer, "example.com/hold", meta.LegacyResourceFinalizer, meta.LegacyResourceFinalizer}
			obj.Finalizers = []string{meta.ControllerFinalizer, meta.ControllerFinalizer, meta.LegacyResourceFinalizer, meta.LegacyResourceFinalizer}
		}},
		{name: "server field ownership", skip: true, change: func(_, obj *corev1.ConfigMap, _ *admission.Request) {
			obj.ManagedFields = []metav1.ManagedFieldsEntry{{Manager: "cleanup"}}
		}},
		{name: "owner", change: func(_, _ *corev1.ConfigMap, req *admission.Request) { req.UserInfo.Username = "alice" }},
		{name: "administrator", change: func(_, _ *corev1.ConfigMap, req *admission.Request) {
			req.UserInfo.Username = "admin"
			req.UserInfo.Groups = []string{"system:masters"}
		}},
		{name: "another tenant service account", change: func(_, _ *corev1.ConfigMap, req *admission.Request) {
			req.UserInfo.Username = "system:serviceaccount:tenant-b:capsule"
		}},
		{name: "another controller service account", change: func(_, _ *corev1.ConfigMap, req *admission.Request) {
			req.UserInfo.Username = "system:serviceaccount:capsule-system:other"
		}},
		{name: "active", change: func(old, obj *corev1.ConfigMap, _ *admission.Request) {
			old.DeletionTimestamp = nil
			obj.DeletionTimestamp = nil
		}},
		{name: "forged deletion", change: func(old, _ *corev1.ConfigMap, _ *admission.Request) { old.DeletionTimestamp = nil }},
		{name: "UID", change: func(_, obj *corev1.ConfigMap, _ *admission.Request) { obj.UID = "replacement" }},
		{name: "empty UID", change: func(old, obj *corev1.ConfigMap, _ *admission.Request) { old.UID = ""; obj.UID = "" }},
		{name: "resource version", change: func(_, obj *corev1.ConfigMap, _ *admission.Request) { obj.ResourceVersion = "2" }},
		{name: "data", change: func(_, obj *corev1.ConfigMap, _ *admission.Request) { obj.Data["profile"] = "changed" }},
		{name: "binary data", change: func(_, obj *corev1.ConfigMap, _ *admission.Request) {
			obj.BinaryData = map[string][]byte{"profile": []byte("changed")}
		}},
		{name: "labels", change: func(_, obj *corev1.ConfigMap, _ *admission.Request) { obj.Labels["profile"] = "changed" }},
		{name: "annotations", change: func(_, obj *corev1.ConfigMap, _ *admission.Request) {
			obj.Annotations = map[string]string{"changed": "true"}
		}},
		{name: "ownership", change: func(_, obj *corev1.ConfigMap, _ *admission.Request) {
			obj.OwnerReferences = []metav1.OwnerReference{{Name: "changed"}}
		}},
		{name: "adding a finalizer", change: func(_, obj *corev1.ConfigMap, _ *admission.Request) {
			obj.Finalizers = []string{meta.ControllerFinalizer, "example.com/new"}
		}},
		{name: "duplicating a finalizer", change: func(_, obj *corev1.ConfigMap, _ *admission.Request) {
			obj.Finalizers = []string{meta.ControllerFinalizer, meta.ControllerFinalizer}
		}},
		{name: "Capsule finalizers", change: func(_, obj *corev1.ConfigMap, _ *admission.Request) { obj.Finalizers = nil }},
		{name: "current lifecycle finalizer", change: func(_, obj *corev1.ConfigMap, _ *admission.Request) {
			obj.Finalizers = []string{meta.LegacyResourceFinalizer}
		}},
		{name: "legacy lifecycle finalizer", change: func(_, obj *corev1.ConfigMap, _ *admission.Request) {
			obj.Finalizers = []string{meta.ControllerFinalizer}
		}},
		{name: "malformed old payload"},
		{name: "malformed new payload"},
		{name: "malformed finalizers"},
		{name: "large numeric data"},
		{name: "no removal", change: func(old, obj *corev1.ConfigMap, _ *admission.Request) {
			obj.Finalizers = append([]string(nil), old.Finalizers...)
		}},
		{name: "status", change: func(_, _ *corev1.ConfigMap, req *admission.Request) { req.SubResource = "status" }},
		{name: "Pod", change: func(_, _ *corev1.ConfigMap, req *admission.Request) { req.Kind.Kind = "Pod" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			scheme := runtime.NewScheme()
			require.NoError(t, corev1.AddToScheme(scheme))
			require.NoError(t, capsulev1beta2.AddToScheme(scheme))
			ns := &corev1.Namespace{Name: "tenant-a", UID: "namespace-uid", OwnerReferences: []metav1.OwnerReference{{APIVersion: capsulev1beta2.GroupVersion.String(), Kind: "Tenant", Name: "tenant-a", UID: "previous-tenant-uid"}}}
			readDone := make(chan struct{}, 1)
			cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(ns, &capsulev1beta2.Tenant{Name: "tenant-a", UID: "replacement-tenant-uid"}).WithInterceptorFuncs(interceptor.Funcs{
				Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
					if tc.skip {
						t.Error("cleanup consumed an admission API read")
					}
					if _, ok := obj.(*capsulev1beta2.RuleStatus); ok {
						defer func() { readDone <- struct{}{} }()
					}
					return c.Get(ctx, key, obj, opts...)
				},
			}).Build()
			old := finalizerRulesObject(1)
			obj := old.DeepCopy()
			obj.Finalizers = []string{meta.ControllerFinalizer, meta.LegacyResourceFinalizer}
			req := finalizerRulesRequest(t, old, obj)
			if tc.change != nil {
				tc.change(old, obj, &req)
				updated := finalizerRulesRequest(t, old, obj)
				req.OldObject, req.Object = updated.OldObject, updated.Object
			}
			switch tc.name {
			case "malformed old payload":
				req.OldObject.Raw = []byte("{")
			case "malformed new payload":
				req.Object.Raw = []byte("{")
			case "malformed finalizers":
				body := &unstructured.Unstructured{}
				require.NoError(t, json.Unmarshal(req.Object.Raw, &body.Object))
				require.NoError(t, unstructured.SetNestedField(body.Object, []any{true}, "metadata", "finalizers"))
				var err error
				req.Object.Raw, err = json.Marshal(body.Object)
				require.NoError(t, err)
			case "large numeric data":
				for i, raw := range []*runtime.RawExtension{&req.OldObject, &req.Object} {
					body := &unstructured.Unstructured{}
					require.NoError(t, body.UnmarshalJSON(raw.Raw))
					require.NoError(t, unstructured.SetNestedField(body.Object, int64(9007199254740992+i), "spec", "limit"))
					var err error
					raw.Raw, err = json.Marshal(body.Object)
					require.NoError(t, err)
				}
			}
			previous, raw := append([]byte(nil), req.OldObject.Raw...), append([]byte(nil), req.Object.Raw...)
			handler := &handlers.TypedTenantWithRulesetHandler[*metav1.PartialObjectMetadata]{Factory: func() *metav1.PartialObjectMetadata { return &metav1.PartialObjectMetadata{} }}
			response := handler.OnUpdate(cl, cl, admission.NewDecoder(scheme), nil)(t.Context(), req)
			if tc.skip {
				require.Nil(t, response, "cleanup continues the admission chain")
			} else {
				require.NotNil(t, response)
				require.False(t, response.Allowed)
				require.Contains(t, response.Result.Message, "ownerReference UID mismatch")
				<-readDone
			}
			require.Equal(t, previous, req.OldObject.Raw)
			require.Equal(t, raw, req.Object.Raw)
		})
	}
}

func BenchmarkRulesetFinalizerAdmission(b *testing.B) {
	b.Setenv(configuration.EnvironmentControllerNamespace, "capsule-system")
	b.Setenv(configuration.EnvironmentServiceaccountName, "capsule")
	for _, count := range []int{1, 32} {
		for _, size := range []int{1, 32} {
			for _, state := range []string{"cleanup", "owner-update", "controller-update", "deny"} {
				b.Run(fmt.Sprintf("%s/tenants=%d/fields=%d", state, count, size), func(b *testing.B) {
					scheme := runtime.NewScheme()
					require.NoError(b, corev1.AddToScheme(scheme))
					require.NoError(b, capsulev1beta2.AddToScheme(scheme))
					objects := []client.Object{&corev1.Namespace{Name: "tenant-a", UID: "namespace-uid", OwnerReferences: []metav1.OwnerReference{{APIVersion: capsulev1beta2.GroupVersion.String(), Kind: "Tenant", Name: "tenant-a", UID: "tenant-a-uid"}}},
						&capsulev1beta2.RuleStatus{Name: meta.NameForManagedRuleStatus(), Namespace: "tenant-a", Status: capsulev1beta2.RuleStatusStatus{Rules: []*rules.NamespaceRuleBodyNamespace{{}}}},
					}
					for i := range count {
						name := fmt.Sprintf("tenant-%d", i)
						if i == 0 {
							name = "tenant-a"
						}
						objects = append(objects, &capsulev1beta2.Tenant{Name: name, UID: types.UID(name + "-uid")})
					}
					cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objects...).Build()
					old := finalizerRulesObject(size)
					obj := old.DeepCopy()
					obj.Finalizers = []string{meta.ControllerFinalizer, meta.LegacyResourceFinalizer}
					req := finalizerRulesRequest(b, old, obj)
					switch state {
					case "owner-update":
						req.UserInfo.Username = "alice"
					case "controller-update":
						old.DeletionTimestamp = nil
						obj.DeletionTimestamp = nil
						req = finalizerRulesRequest(b, old, obj)
					case "deny":
						obj.Data["profile"] = "changed"
						req = finalizerRulesRequest(b, old, obj)
						current := &capsulev1beta2.Tenant{}
						require.NoError(b, cl.Get(b.Context(), client.ObjectKey{Name: "tenant-a"}, current))
						require.NoError(b, cl.Delete(b.Context(), current))
						require.NoError(b, cl.Create(b.Context(), &capsulev1beta2.Tenant{Name: "tenant-a", UID: "replacement-uid"}))
					}
					handler := &handlers.TypedTenantWithRulesetHandler[*unstructured.Unstructured]{Factory: func() *unstructured.Unstructured { return &unstructured.Unstructured{} }}
					calls := &mockclient.CallCounter{}
					reader := interceptor.NewClient(cl, calls.Interceptors())
					fn := handler.OnUpdate(reader, reader, admission.NewDecoder(scheme), nil)
					b.ReportAllocs()
					for b.Loop() {
						response := fn(b.Context(), req)
						if state == "deny" {
							require.NotNil(b, response)
							require.False(b, response.Allowed)
						} else {
							require.Nil(b, response)
						}
					}
					calls.Report(b)
				})
			}
		}
	}
}

func BenchmarkRulesetFinalizerAdmissionLongLists(b *testing.B) {
	b.Setenv(configuration.EnvironmentControllerNamespace, "capsule-system")
	b.Setenv(configuration.EnvironmentServiceaccountName, "capsule")
	for _, count := range []int{1, 64, 1024} {
		b.Run(fmt.Sprintf("lifecycle-finalizers=%d", count*2), func(b *testing.B) {
			old := finalizerRulesObject(1)
			old.Finalizers = nil
			for range count {
				old.Finalizers = append(old.Finalizers, meta.ControllerFinalizer)
			}
			old.Finalizers = append(old.Finalizers, "example.com/hold")
			for range count {
				old.Finalizers = append(old.Finalizers, meta.LegacyResourceFinalizer)
			}
			obj := old.DeepCopy()
			obj.Finalizers = append(append([]string(nil), old.Finalizers[:count]...), old.Finalizers[count+1:]...)
			req := finalizerRulesRequest(b, old, obj)
			handler := &handlers.TypedTenantWithRulesetHandler[*unstructured.Unstructured]{}
			fn := handler.OnUpdate(nil, nil, nil, nil)
			b.ReportAllocs()
			for b.Loop() {
				require.Nil(b, fn(b.Context(), req))
			}
			b.ReportMetric(0, "GET/op")
		})
	}
}
