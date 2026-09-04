// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package serviceaccounts

import (
	"context"
	"strings"
	"testing"

	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	serviceaccountindexer "github.com/projectcapsule/capsule/pkg/runtime/indexers/serviceaccount"
	"github.com/projectcapsule/capsule/pkg/runtime/indexers/tenantresource"
)

func TestReferenceProtectionOnDelete(t *testing.T) {
	t.Parallel()

	reference := &meta.NamespacedRFC1123ObjectReferenceWithNamespace{
		Name:      "runner",
		Namespace: "capsule-system",
	}

	tests := []struct {
		name       string
		objects    []client.Object
		wantDenied string
	}{
		{name: "allows an unreferenced ServiceAccount"},
		{
			name: "denies an unexpired BreakRequest reference",
			objects: []client.Object{&capsulev1beta2.BreakRequest{
				ObjectMeta: metav1.ObjectMeta{Name: "temporary-access", Namespace: "team-a"},
				Status: capsulev1beta2.BreakRequestStatus{
					Phase: capsulev1beta2.RequestPhaseActive,
					Request: &capsulev1beta2.BreakRequestStatusRequest{
						Impersonation: reference.DeepCopy(),
					},
				},
			}},
			wantDenied: "unexpired BreakRequest team-a/temporary-access",
		},
		{
			name: "allows an expired BreakRequest reference",
			objects: []client.Object{&capsulev1beta2.BreakRequest{
				ObjectMeta: metav1.ObjectMeta{Name: "expired-access", Namespace: "team-a"},
				Status: capsulev1beta2.BreakRequestStatus{
					Phase: capsulev1beta2.RequestPhaseExpired,
					Request: &capsulev1beta2.BreakRequestStatusRequest{
						Impersonation: reference.DeepCopy(),
					},
				},
			}},
		},
		{
			name: "denies a GlobalTenantResource reference",
			objects: []client.Object{&capsulev1beta2.GlobalTenantResource{
				ObjectMeta: metav1.ObjectMeta{Name: "global-distribution"},
				Status: capsulev1beta2.GlobalTenantResourceStatus{
					TenantResourceCommonStatus: capsulev1beta2.TenantResourceCommonStatus{
						ServiceAccount: reference.DeepCopy(),
					},
				},
			}},
			wantDenied: "GlobalTenantResource global-distribution",
		},
		{
			name: "denies a TenantResource reference",
			objects: []client.Object{&capsulev1beta2.TenantResource{
				ObjectMeta: metav1.ObjectMeta{Name: "namespace-distribution", Namespace: "capsule-system"},
				Status: capsulev1beta2.TenantResourceStatus{
					TenantResourceCommonStatus: capsulev1beta2.TenantResourceCommonStatus{
						ServiceAccount: reference.DeepCopy(),
					},
				},
			}},
			wantDenied: "TenantResource capsule-system/namespace-distribution",
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			cl := referenceProtectionFakeClient(t, testCase.objects...)
			handler := ReferenceProtection()
			response := handler.OnDelete(cl, cl, nil, nil)(context.Background(), admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Operation: admissionv1.Delete,
					Namespace: "capsule-system",
					Name:      "runner",
				},
			})

			if testCase.wantDenied == "" {
				if response != nil {
					t.Fatalf("OnDelete() = %#v, want allowed", response)
				}

				return
			}

			if response == nil || response.Allowed {
				t.Fatalf("OnDelete() = %#v, want denial", response)
			}
			if response.Result == nil || !strings.Contains(response.Result.Message, testCase.wantDenied) {
				t.Fatalf("OnDelete() message = %#v, want containing %q", response.Result, testCase.wantDenied)
			}
		})
	}
}

func referenceProtectionFakeClient(t *testing.T, objects ...client.Object) client.Client {
	t.Helper()

	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	if err := capsulev1beta2.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}

	breakRequestIndexer := serviceaccountindexer.BreakRequestReference{}
	globalResourceIndexer := tenantresource.GlobalServiceAccount{}
	tenantResourceIndexer := tenantresource.NamespacedServiceAccount{}

	return fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(objects...).
		WithIndex(breakRequestIndexer.Object(), breakRequestIndexer.Field(), breakRequestIndexer.Func()).
		WithIndex(globalResourceIndexer.Object(), globalResourceIndexer.Field(), globalResourceIndexer.Func()).
		WithIndex(tenantResourceIndexer.Object(), tenantResourceIndexer.Field(), tenantResourceIndexer.Func()).
		Build()
}
