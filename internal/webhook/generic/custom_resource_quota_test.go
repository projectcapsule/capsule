// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package generic

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/runtime/events"
	tenantindex "github.com/projectcapsule/capsule/pkg/runtime/indexers/tenant"
)

const quotaTestResource = "widgets.example.com_v1"

func legacyQuotaFixture(t testing.TB, tenants int) client.WithWatch {
	t.Helper()
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))
	require.NoError(t, capsulev1beta2.AddToScheme(scheme))
	objects := make([]client.Object, 0, tenants*2)
	for i := range tenants {
		name := fmt.Sprintf("tenant-%d", i)
		tnt := &capsulev1beta2.Tenant{Name: name, UID: types.UID(name), Annotations: map[string]string{capsulev1beta2.LimitAnnotationForResource(quotaTestResource): "3", capsulev1beta2.UsedAnnotationForResource(quotaTestResource): "1"}}
		ns := &corev1.Namespace{Name: name + "-ns", UID: types.UID(name + "-ns"), OwnerReferences: []metav1.OwnerReference{{APIVersion: capsulev1beta2.GroupVersion.String(), Kind: "Tenant", Name: name, UID: tnt.UID}}}
		objects = append(objects, tnt, ns)
	}
	return fake.NewClientBuilder().WithScheme(scheme).WithObjects(objects...).WithStatusSubresource(&capsulev1beta2.Tenant{}).WithIndex(&capsulev1beta2.Tenant{}, tenantindex.NamespaceIndexerFieldName, func(obj client.Object) []string { return obj.(*capsulev1beta2.Tenant).Status.Namespaces }).Build()
}

func legacyQuotaRequest(operation admissionv1.Operation) admission.Request {
	return admission.Request{Operation: operation, Namespace: "tenant-0-ns", Name: "widget", Resource: metav1.GroupVersionResource{Group: "example.com", Version: "v1", Resource: "widgets"}}
}

func invokeLegacyQuota(ctx context.Context, c client.Client, reader client.Reader, req admission.Request) *admission.Response {
	handler := ResourceCounterHandler(c)
	recorder := events.NewDiscardRecorder()
	switch req.Operation {
	case admissionv1.Create:
		return handler.OnCreate(c, reader, nil, recorder)(ctx, req)
	case admissionv1.Delete:
		return handler.OnDelete(c, reader, nil, recorder)(ctx, req)
	default:
		return handler.OnUpdate(c, reader, nil, recorder)(ctx, req)
	}
}

func TestLegacyQuotaUsesAuthoritativeNamespaceOwnership(t *testing.T) {
	for _, operation := range []admissionv1.Operation{admissionv1.Create, admissionv1.Delete} {
		for _, mode := range []string{"pending-membership", "stale-foreign-membership", "visible-membership", "zero-limit", "unlimited", "malformed-limit", "dry-run", "no-counter", "no-namespace", "subresource", "update"} {
			t.Run(string(operation)+"/"+mode, func(t *testing.T) {
				ctx := t.Context()
				base := legacyQuotaFixture(t, 2)
				current, foreign := &capsulev1beta2.Tenant{}, &capsulev1beta2.Tenant{}
				require.NoError(t, base.Get(ctx, client.ObjectKey{Name: "tenant-0"}, current))
				require.NoError(t, base.Get(ctx, client.ObjectKey{Name: "tenant-1"}, foreign))
				switch mode {
				case "visible-membership":
					current.Status.Namespaces = []string{"tenant-0-ns"}
					require.NoError(t, base.Status().Update(ctx, current))
				case "stale-foreign-membership":
					foreign.Status.Namespaces = []string{"tenant-0-ns"}
					require.NoError(t, base.Status().Update(ctx, foreign))
				case "zero-limit":
					current.Annotations[capsulev1beta2.LimitAnnotationForResource(quotaTestResource)] = "0"
				case "unlimited":
					delete(current.Annotations, capsulev1beta2.LimitAnnotationForResource(quotaTestResource))
				case "malformed-limit":
					current.Annotations[capsulev1beta2.LimitAnnotationForResource(quotaTestResource)] = "invalid"
				case "no-counter":
					delete(current.Annotations, capsulev1beta2.UsedAnnotationForResource(quotaTestResource))
				}
				require.NoError(t, base.Update(ctx, current))
				foreignBefore := foreign.DeepCopy()
				var reads, writes, lists int
				c := interceptor.NewClient(base, interceptor.Funcs{
					Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
						reads++
						return c.Get(ctx, key, obj, opts...)
					},
					List: func(ctx context.Context, c client.WithWatch, obj client.ObjectList, opts ...client.ListOption) error {
						lists++
						return c.List(ctx, obj, opts...)
					},
					Update: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.UpdateOption) error {
						writes++
						return c.Update(ctx, obj, opts...)
					},
				})
				// The reader and writer intentionally differ: admission must use its API reader.
				cached := interceptor.NewClient(c, interceptor.Funcs{Get: func(context.Context, client.WithWatch, client.ObjectKey, client.Object, ...client.GetOption) error {
					t.Fatal("cached Get must not resolve ownership")
					return nil
				}})
				req := legacyQuotaRequest(operation)
				switch mode {
				case "dry-run":
					req.DryRun = new(true)
				case "no-namespace":
					req.Namespace = ""
				case "subresource":
					req.SubResource = "status"
				case "update":
					req.Operation = admissionv1.Update
				}
				result := invokeLegacyQuota(ctx, cached, c, req)
				denied := operation == admissionv1.Create && (mode == "zero-limit" || mode == "malformed-limit")
				if denied {
					require.NotNil(t, result)
					require.False(t, result.Allowed)
					if mode == "zero-limit" {
						require.Contains(t, result.Result.Message, "has reached quota limit of 0 items")
					} else {
						require.Contains(t, result.Result.Message, "limit cannot be parsed")
					}
				} else {
					require.Nil(t, result)
				}
				skip := mode == "no-namespace" || mode == "subresource" || mode == "update"
				expectedReads := 2
				if skip {
					expectedReads = 0
				}
				require.Equal(t, expectedReads, reads)
				require.Zero(t, lists)
				changed := !skip && !denied && mode != "dry-run" && !(operation == admissionv1.Create && mode == "unlimited") && !(operation == admissionv1.Delete && mode == "no-counter")
				expectedWrites := 0
				if changed {
					expectedWrites = 1
				}
				require.Equal(t, expectedWrites, writes)
				actual := &capsulev1beta2.Tenant{}
				require.NoError(t, base.Get(ctx, client.ObjectKeyFromObject(current), actual))
				used := "1"
				if mode == "no-counter" {
					used = ""
				}
				if changed {
					if operation == admissionv1.Create {
						if used == "" {
							used = "1"
						} else {
							used = "2"
						}
					} else {
						used = "0"
					}
				}
				require.Equal(t, used, actual.Annotations[capsulev1beta2.UsedAnnotationForResource(quotaTestResource)])
				require.NoError(t, base.Get(ctx, client.ObjectKeyFromObject(foreign), actual))
				require.Equal(t, foreignBefore, actual, "quota must not affect another tenant even if its status claims this namespace")
			})
		}
	}
}

func TestLegacyQuotaOwnershipErrorsAndRetries(t *testing.T) {
	for _, operation := range []admissionv1.Operation{admissionv1.Create, admissionv1.Delete} {
		for _, mode := range []string{"namespace-error", "tenant-error", "missing-tenant", "recreated-tenant", "missing-namespace", "unowned", "conflict", "limit-changed", "ownership-changed", "tenant-recreated-on-retry", "namespace-recreated-on-retry", "update-error", "cancelled"} {
			t.Run(string(operation)+"/"+mode, func(t *testing.T) {
				base := legacyQuotaFixture(t, 2)
				ctx := t.Context()
				failure := errors.New("API unavailable")
				ns := &corev1.Namespace{}
				require.NoError(t, base.Get(ctx, client.ObjectKey{Name: "tenant-0-ns"}, ns))
				if mode == "recreated-tenant" {
					ns.OwnerReferences[0].UID = "previous-tenant"
					require.NoError(t, base.Update(ctx, ns))
				}
				if mode == "unowned" {
					ns.OwnerReferences = nil
					require.NoError(t, base.Update(ctx, ns))
				}
				if mode == "missing-namespace" {
					require.NoError(t, base.Delete(ctx, ns))
				}
				if mode == "missing-tenant" {
					require.NoError(t, base.Delete(ctx, &capsulev1beta2.Tenant{Name: "tenant-0"}))
				}
				if mode == "cancelled" {
					cancelCtx, cancel := context.WithCancel(ctx)
					cancel()
					ctx = cancelCtx
				}
				var gets, writes int
				c := interceptor.NewClient(base, interceptor.Funcs{
					Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
						gets++
						if err := ctx.Err(); err != nil {
							return err
						}
						if _, ok := obj.(*corev1.Namespace); ok && mode == "namespace-error" {
							return failure
						}
						if _, ok := obj.(*capsulev1beta2.Tenant); ok && mode == "tenant-error" {
							return failure
						}
						return c.Get(ctx, key, obj, opts...)
					},
					List: func(context.Context, client.WithWatch, client.ObjectList, ...client.ListOption) error {
						t.Fatal("quota must not list tenants")
						return nil
					},
					Update: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.UpdateOption) error {
						writes++
						if mode == "update-error" {
							return failure
						}
						if writes == 1 {
							switch mode {
							case "conflict", "limit-changed", "ownership-changed", "tenant-recreated-on-retry", "namespace-recreated-on-retry":
								latest := &capsulev1beta2.Tenant{}
								require.NoError(t, c.Get(ctx, client.ObjectKey{Name: "tenant-0"}, latest))
								latest.Annotations[capsulev1beta2.UsedAnnotationForResource(quotaTestResource)] = "2"
								if mode == "limit-changed" {
									latest.Annotations[capsulev1beta2.LimitAnnotationForResource(quotaTestResource)] = "2"
								}
								if mode == "tenant-recreated-on-retry" {
									latest.UID = "replacement"
								}
								require.NoError(t, c.Update(ctx, latest))
								if mode == "namespace-recreated-on-retry" {
									ns.UID = "replacement"
									require.NoError(t, c.Update(ctx, ns))
								}
								if mode == "ownership-changed" {
									ns.OwnerReferences[0].Name, ns.OwnerReferences[0].UID = "tenant-1", "tenant-1"
									require.NoError(t, c.Update(ctx, ns))
								}
								return apierrors.NewConflict(schema.GroupResource{Group: capsulev1beta2.GroupVersion.Group, Resource: "tenants"}, "tenant-0", errors.New("retry"))
							}
						}
						return c.Update(ctx, obj, opts...)
					},
				})
				result := invokeLegacyQuota(ctx, c, c, legacyQuotaRequest(operation))
				allow := mode == "missing-namespace" || mode == "unowned" || mode == "conflict" || (mode == "limit-changed" && operation == admissionv1.Delete)
				if allow {
					require.Nil(t, result)
				} else {
					require.NotNil(t, result)
					require.False(t, result.Allowed)
					expected := map[string]string{"namespace-error": "API unavailable", "tenant-error": "API unavailable", "missing-tenant": "cannot resolve tenant owner", "recreated-tenant": "UID mismatch", "ownership-changed": "ownership changed", "tenant-recreated-on-retry": "UID mismatch", "namespace-recreated-on-retry": "ownership changed", "update-error": "API unavailable", "cancelled": "context canceled", "limit-changed": "has reached quota limit of 2 items"}[mode]
					require.Contains(t, result.Result.Message, expected)
				}
				if mode == "conflict" || mode == "limit-changed" || mode == "ownership-changed" || mode == "tenant-recreated-on-retry" || mode == "namespace-recreated-on-retry" {
					require.Equal(t, 4, gets)
					expectedWrites := 1
					if mode == "conflict" || (mode == "limit-changed" && operation == admissionv1.Delete) {
						expectedWrites = 2
					}
					require.Equal(t, expectedWrites, writes)
				}
				actual := &capsulev1beta2.Tenant{}
				require.NoError(t, base.Get(t.Context(), client.ObjectKey{Name: "tenant-1"}, actual))
				require.Equal(t, "1", actual.Annotations[capsulev1beta2.UsedAnnotationForResource(quotaTestResource)])
				if mode == "conflict" {
					require.NoError(t, base.Get(t.Context(), client.ObjectKey{Name: "tenant-0"}, actual))
					expected := "3"
					if operation == admissionv1.Delete {
						expected = "1"
					}
					require.Equal(t, expected, actual.Annotations[capsulev1beta2.UsedAnnotationForResource(quotaTestResource)], "conflict must refresh usage before mutating")
				}
			})
		}
	}
}

func TestLegacyQuotaConcurrentReservations(t *testing.T) {
	base := legacyQuotaFixture(t, 2)
	var admitted atomic.Int32
	done := make(chan struct{}, 2)
	for range 2 {
		go func() {
			defer func() { done <- struct{}{} }()
			if response := invokeLegacyQuota(t.Context(), base, base, legacyQuotaRequest(admissionv1.Create)); response == nil {
				admitted.Add(1)
			} else {
				if response.Allowed || !strings.Contains(response.Result.Message, "quota limit") {
					t.Errorf("unexpected concurrent response: %+v", response)
				}
			}
		}()
	}
	for range 2 {
		<-done
	}
	require.Equal(t, int32(2), admitted.Load())
	tnt := &capsulev1beta2.Tenant{}
	require.NoError(t, base.Get(t.Context(), client.ObjectKey{Name: "tenant-0"}, tnt))
	require.Equal(t, "3", tnt.Annotations[capsulev1beta2.UsedAnnotationForResource(quotaTestResource)])
	result := invokeLegacyQuota(t.Context(), base, base, legacyQuotaRequest(admissionv1.Create))
	require.NotNil(t, result)
	require.Contains(t, result.Result.Message, "quota limit of 3")
}
