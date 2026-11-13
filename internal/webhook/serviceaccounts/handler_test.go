// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package serviceaccounts

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	admissionv1 "k8s.io/api/admission/v1"
	authenticationv1 "k8s.io/api/authentication/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/api/rbac"
	"github.com/projectcapsule/capsule/pkg/runtime/configuration"
	"github.com/projectcapsule/capsule/pkg/runtime/events"
)

func TestHandlerSkipsUnlabeledServiceAccountsBeforeTenantLookup(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}

	base := fake.NewClientBuilder().WithScheme(scheme).Build()
	reader := &serviceAccountCountingReader{Reader: base}
	handler := Handler(nil)
	request := serviceAccountAdmissionRequest(t, &corev1.ServiceAccount{})

	if response := handler.OnCreate(nil, reader, admission.NewDecoder(scheme), nil)(
		context.Background(),
		request,
	); response != nil {
		t.Fatalf("response = %#v, want nil", response)
	}

	if reader.gets != 0 {
		t.Fatalf("tenant lookup gets = %d, want 0 without promotion labels", reader.gets)
	}
}

type serviceAccountCountingReader struct {
	client.Reader

	gets int
}

func (r *serviceAccountCountingReader) Get(
	ctx context.Context,
	key client.ObjectKey,
	obj client.Object,
	opts ...client.GetOption,
) error {
	r.gets++

	return r.Reader.Get(ctx, key, obj, opts...)
}

func serviceAccountAdmissionRequest(t *testing.T, sa *corev1.ServiceAccount) admission.Request {
	t.Helper()

	raw, err := json.Marshal(sa)
	if err != nil {
		t.Fatal(err)
	}

	return admission.Request{
		Operation: admissionv1.Create,
		Namespace: "solar",
		Object: runtime.RawExtension{
			Raw: raw,
		}}
}

// promotionStore is a fake store whose reads the test counts, so it can tell
// the manager cache from the API reader.
func promotionStore(t *testing.T, scheme *runtime.Scheme, objects ...client.Object) (client.Client, *int) {
	t.Helper()

	reads := new(int)
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objects...).WithInterceptorFuncs(interceptor.Funcs{
		Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
			*reads++

			return c.Get(ctx, key, obj, opts...)
		},
		List: func(ctx context.Context, c client.WithWatch, list client.ObjectList, opts ...client.ListOption) error {
			*reads++

			return c.List(ctx, list, opts...)
		},
	}).Build()

	return c, reads
}

// TestPromotionReadsTheTenantFromTheAPIReader covers an owner whom the
// manager cache still lists in the Tenant status after the API server
// dropped them. Promotion is an authorization decision: the Tenant comes
// from the API reader only, and a revoked owner is denied.
func TestPromotionReadsTheTenantFromTheAPIReader(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}

	if err := capsulev1beta2.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}

	config := &capsulev1beta2.CapsuleConfiguration{Name: "capsule"}
	config.Spec.AllowServiceAccountPromotion = true
	configClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(config).Build()
	cfg := configuration.NewCapsuleConfiguration(t.Context(), configClient, configClient, nil, "capsule")

	solar := func(owners ...string) *capsulev1beta2.Tenant {
		tnt := &capsulev1beta2.Tenant{Name: "solar", UID: "solar-uid"}
		tnt.Spec.Permissions.AllowOwnerPromotion = true

		for _, owner := range owners {
			tnt.Status.Owners = append(tnt.Status.Owners, rbac.CoreOwnerSpec{UserSpec: rbac.UserSpec{Kind: rbac.UserOwner, Name: owner}})
		}

		return tnt
	}
	namespace := &corev1.Namespace{Name: "solar", UID: "solar-ns-uid", OwnerReferences: []metav1.OwnerReference{{
		APIVersion: capsulev1beta2.GroupVersion.String(), Kind: "Tenant", Name: "solar", UID: "solar-uid",
	}}}
	denied := "alice not allowed to promote serviceaccount to tenant owner"

	for _, tc := range []struct {
		name      string
		label     string
		apiOwners []string
		want      string
	}{
		{name: "owner promotion by an owner", label: meta.OwnerPromotionLabel, apiOwners: []string{"alice"}},
		{name: "owner promotion by a revoked owner", label: meta.OwnerPromotionLabel, want: denied},
		{name: "promotion by an owner", label: meta.ServiceAccountPromotionLabel, apiOwners: []string{"alice"}},
		{name: "promotion by a revoked owner", label: meta.ServiceAccountPromotionLabel, want: denied},
	} {
		for _, operation := range []admissionv1.Operation{admissionv1.Create, admissionv1.Update} {
			t.Run(tc.name+"/"+string(operation), func(t *testing.T) {
				t.Parallel()

				// The cache still lists alice as an owner of the Tenant.
				cache, cacheReads := promotionStore(t, scheme, solar("alice"), namespace.DeepCopy())
				api, apiReads := promotionStore(t, scheme, solar(tc.apiOwners...), namespace.DeepCopy())

				sa := &corev1.ServiceAccount{Name: "robot", Namespace: "solar", Labels: map[string]string{tc.label: meta.ValueTrue}}
				request := serviceAccountAdmissionRequest(t, sa)
				request.Operation = operation
				request.UserInfo = authenticationv1.UserInfo{Username: "alice"}

				handler := Handler(cfg, Promotion(cfg), OwnerPromotion(cfg))
				decoder := admission.NewDecoder(scheme)
				fn := handler.OnCreate(cache, api, decoder, events.NewDiscardRecorder())

				if operation == admissionv1.Update {
					old := sa.DeepCopy()
					old.Labels = nil

					raw, err := json.Marshal(old)
					if err != nil {
						t.Fatal(err)
					}

					request.OldObject = runtime.RawExtension{Raw: raw}
					fn = handler.OnUpdate(cache, api, decoder, events.NewDiscardRecorder())
				}

				response := fn(t.Context(), request)

				if tc.want == "" {
					if response != nil {
						t.Fatalf("response = %#v, want the owner of the API Tenant allowed", response)
					}
				} else if response == nil || response.Allowed || !strings.Contains(response.Result.Message, tc.want) {
					t.Fatalf("response = %#v, want a denial with %q", response, tc.want)
				}

				if *cacheReads != 0 {
					t.Fatalf("cache reads = %d, want 0: promotion reads only the API", *cacheReads)
				}

				if *apiReads != 2 {
					t.Fatalf("API reads = %d, want the Namespace and its Tenant", *apiReads)
				}
			})
		}
	}
}
