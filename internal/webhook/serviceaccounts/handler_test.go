// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package serviceaccounts

import (
	"context"
	"encoding/json"
	"testing"

	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
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

	return admission.Request{AdmissionRequest: admissionv1.AdmissionRequest{
		Operation: admissionv1.Create,
		Namespace: "solar",
		Object: runtime.RawExtension{
			Raw: raw,
		},
	}}
}
