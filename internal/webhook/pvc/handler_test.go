// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package pvc

import (
	"context"
	"encoding/json"
	"testing"

	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

func TestMutatingHandlerSkipsDynamicClaimsBeforeTenantLookup(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}

	base := fake.NewClientBuilder().WithScheme(scheme).Build()
	reader := &pvcCountingReader{Reader: base}
	handler := MutatingHandler()
	request := pvcAdmissionRequest(t, &corev1.PersistentVolumeClaim{})

	if response := handler.OnCreate(nil, reader, admission.NewDecoder(scheme), nil)(
		context.Background(),
		request,
	); response != nil {
		t.Fatalf("response = %#v, want nil", response)
	}

	if reader.gets != 0 {
		t.Fatalf("tenant lookup gets = %d, want 0 for a dynamic claim", reader.gets)
	}
}

func TestValidatingHandlerSkipsTerminatingClaimWithUnchangedSpec(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}

	now := metav1.Now()
	oldPVC := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			DeletionTimestamp: &now,
			Finalizers:        []string{"kubernetes.io/pvc-protection"},
		},
		Spec: corev1.PersistentVolumeClaimSpec{VolumeName: "caladan"},
	}
	newPVC := oldPVC.DeepCopy()
	newPVC.Finalizers = nil

	base := fake.NewClientBuilder().WithScheme(scheme).Build()
	reader := &pvcCountingReader{Reader: base}
	handler := Handler(PersistentVolumeValidatingVolume())
	request := pvcUpdateAdmissionRequest(t, oldPVC, newPVC)

	if response := handler.OnUpdate(nil, reader, admission.NewDecoder(scheme), nil)(
		context.Background(),
		request,
	); response != nil {
		t.Fatalf("response = %#v, want nil", response)
	}

	if reader.gets != 0 {
		t.Fatalf("admission gets = %d, want 0 for terminating claim cleanup", reader.gets)
	}
}

func TestRequiresPVCSpecValidation(t *testing.T) {
	t.Parallel()

	now := metav1.Now()
	terminating := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{DeletionTimestamp: &now},
		Spec:       corev1.PersistentVolumeClaimSpec{VolumeName: "caladan"},
	}
	changed := terminating.DeepCopy()
	changed.Spec.VolumeName = "salusa"
	active := terminating.DeepCopy()
	active.DeletionTimestamp = nil

	tests := []struct {
		name      string
		operation admissionv1.Operation
		oldPVC    *corev1.PersistentVolumeClaim
		pvc       *corev1.PersistentVolumeClaim
		want      bool
	}{
		{
			name:      "create",
			operation: admissionv1.Create,
			pvc:       active,
			want:      true,
		},
		{
			name:      "active update",
			operation: admissionv1.Update,
			oldPVC:    active.DeepCopy(),
			pvc:       active,
			want:      true,
		},
		{
			name:      "terminating finalizer update",
			operation: admissionv1.Update,
			oldPVC:    terminating.DeepCopy(),
			pvc:       terminating,
			want:      false,
		},
		{
			name:      "terminating spec update",
			operation: admissionv1.Update,
			oldPVC:    terminating.DeepCopy(),
			pvc:       changed,
			want:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := admission.Request{AdmissionRequest: admissionv1.AdmissionRequest{
				Operation: tt.operation,
			}}

			if got := requiresPVCSpecValidation(req, tt.pvc, tt.oldPVC); got != tt.want {
				t.Fatalf("requiresPVCSpecValidation() = %t, want %t", got, tt.want)
			}
		})
	}
}

type pvcCountingReader struct {
	client.Reader

	gets int
}

func (r *pvcCountingReader) Get(
	ctx context.Context,
	key client.ObjectKey,
	obj client.Object,
	opts ...client.GetOption,
) error {
	r.gets++

	return r.Reader.Get(ctx, key, obj, opts...)
}

func pvcAdmissionRequest(t *testing.T, pvc *corev1.PersistentVolumeClaim) admission.Request {
	t.Helper()

	raw, err := json.Marshal(pvc)
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

func pvcUpdateAdmissionRequest(
	t *testing.T,
	oldPVC *corev1.PersistentVolumeClaim,
	pvc *corev1.PersistentVolumeClaim,
) admission.Request {
	t.Helper()

	request := pvcAdmissionRequest(t, pvc)
	request.Operation = admissionv1.Update

	raw, err := json.Marshal(oldPVC)
	if err != nil {
		t.Fatal(err)
	}

	request.OldObject = runtime.RawExtension{Raw: raw}

	return request
}
