// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package pvc

import (
	"context"
	"encoding/json"
	"testing"

	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/runtime/handlers"
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

func TestHandlersSkipBoundClaimsBeforeTenantLookup(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}

	oldPVC := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{Name: "nginx-logs"},
		Spec: corev1.PersistentVolumeClaimSpec{
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{
				"velero.io/dynamic-pv-restore": "test.nginx-logs.sg75p",
			}},
			VolumeName: "pvc-f9bc7e8d",
		},
		Status: corev1.PersistentVolumeClaimStatus{Phase: corev1.ClaimBound},
	}
	newPVC := oldPVC.DeepCopy()
	newPVC.Labels = map[string]string{"test": "test"}

	tests := []struct {
		name    string
		handler handlers.Handler
	}{
		{
			name:    "mutating",
			handler: MutatingHandler(PersistentVolumeMutatingVolume()),
		},
		{
			name:    "validating",
			handler: Handler(PersistentVolumeValidatingVolume()),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			base := fake.NewClientBuilder().WithScheme(scheme).Build()
			reader := &pvcCountingReader{Reader: base}
			request := pvcUpdateAdmissionRequest(t, oldPVC, newPVC)

			if response := tt.handler.OnUpdate(nil, reader, admission.NewDecoder(scheme), nil)(
				context.Background(),
				request,
			); response != nil {
				t.Fatalf("response = %#v, want nil", response)
			}

			if reader.gets != 0 {
				t.Fatalf("tenant lookup gets = %d, want 0 for a bound claim", reader.gets)
			}
		})
	}
}

func TestVolumeHooksSkipBoundClaimUpdates(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}

	oldPVC := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{Name: "nginx-logs"},
		Spec: corev1.PersistentVolumeClaimSpec{
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{
				"velero.io/dynamic-pv-restore": "test.nginx-logs.sg75p",
			}},
			VolumeName: "pvc-f9bc7e8d",
		},
		Status: corev1.PersistentVolumeClaimStatus{Phase: corev1.ClaimBound},
	}
	newPVC := oldPVC.DeepCopy()
	newPVC.Labels = map[string]string{"test": "test"}
	tnt := &capsulev1beta2.Tenant{ObjectMeta: metav1.ObjectMeta{Name: "test"}}
	request := pvcUpdateAdmissionRequest(t, oldPVC, newPVC)
	decoder := admission.NewDecoder(scheme)

	if response := PersistentVolumeMutatingVolume().OnUpdate(
		nil,
		nil,
		oldPVC,
		newPVC,
		decoder,
		nil,
		tnt,
	)(context.Background(), request); response != nil {
		t.Fatalf("mutating response = %#v, want nil", response)
	}

	if len(newPVC.Spec.Selector.MatchExpressions) != 0 {
		t.Fatalf("selector expressions = %#v, want unchanged", newPVC.Spec.Selector.MatchExpressions)
	}

	if response := PersistentVolumeValidatingVolume().OnUpdate(
		nil,
		nil,
		oldPVC,
		newPVC,
		decoder,
		nil,
		tnt,
	)(context.Background(), request); response != nil {
		t.Fatalf("validating response = %#v, want nil", response)
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
	bound := active.DeepCopy()
	bound.Status.Phase = corev1.ClaimBound
	resizedBound := bound.DeepCopy()
	resizedBound.Spec.Resources.Requests = corev1.ResourceList{
		corev1.ResourceStorage: resource.MustParse("2Gi"),
	}

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
			name:      "bound metadata update",
			operation: admissionv1.Update,
			oldPVC:    bound.DeepCopy(),
			pvc:       bound,
			want:      false,
		},
		{
			name:      "bound resize",
			operation: admissionv1.Update,
			oldPVC:    bound.DeepCopy(),
			pvc:       resizedBound,
			want:      false,
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
