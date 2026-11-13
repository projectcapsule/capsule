// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package pvc

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/runtime/events"
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
		DeletionTimestamp: &now,
		Finalizers:        []string{"kubernetes.io/pvc-protection"},
		Spec:              corev1.PersistentVolumeClaimSpec{VolumeName: "caladan"},
	}
	newPVC := oldPVC.DeepCopy()
	newPVC.Finalizers = nil

	base := fake.NewClientBuilder().WithScheme(scheme).Build()
	reader := &pvcCountingReader{Reader: base}
	handler := Handler(PersistentVolumeValidatingVolume(nil))
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
		Name: "nginx-logs",
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
			handler: Handler(PersistentVolumeValidatingVolume(nil)),
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
		Name: "nginx-logs",
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
	tnt := &capsulev1beta2.Tenant{Name: "test"}
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

	if response := PersistentVolumeValidatingVolume(nil).OnUpdate(
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
		DeletionTimestamp: &now,
		Spec:              corev1.PersistentVolumeClaimSpec{VolumeName: "caladan"},
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
			req := admission.Request{
				Operation: tt.operation}

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

	return admission.Request{
		Operation: admissionv1.Create,
		Namespace: "solar",
		Object: runtime.RawExtension{
			Raw: raw,
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

// pvcTenantStore is a fake store whose reads the test counts, so it can tell
// the manager cache from the API reader.
func pvcTenantStore(t *testing.T, scheme *runtime.Scheme, objects ...client.Object) (client.Client, *int) {
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

// TestVolumeTenantChecksReadTheTenantFromTheAPIReader covers a Namespace that
// the manager cache still sees in Tenant oil while the API server has it in
// Tenant gas. The PersistentVolume tenant checks resolve the Tenant from the
// API reader only: a volume of oil stays out of reach.
func TestVolumeTenantChecksReadTheTenantFromTheAPIReader(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}

	if err := capsulev1beta2.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}

	oil := &capsulev1beta2.Tenant{Name: "oil", UID: "oil-uid"}
	gas := &capsulev1beta2.Tenant{Name: "gas", UID: "gas-uid"}
	namespace := func(owner *capsulev1beta2.Tenant) *corev1.Namespace {
		return &corev1.Namespace{Name: "solar", UID: "solar-uid", OwnerReferences: []metav1.OwnerReference{{
			APIVersion: capsulev1beta2.GroupVersion.String(), Kind: "Tenant", Name: owner.Name, UID: owner.UID,
		}}}
	}
	volume := func(owner *capsulev1beta2.Tenant) *corev1.PersistentVolume {
		return &corev1.PersistentVolume{Name: owner.Name + "-volume", UID: types.UID(owner.Name + "-volume-uid"), Labels: map[string]string{meta.TenantLabel: owner.Name}}
	}
	claim := func(volumeName string) *corev1.PersistentVolumeClaim {
		return &corev1.PersistentVolumeClaim{Name: "data", Namespace: "solar", Spec: corev1.PersistentVolumeClaimSpec{VolumeName: volumeName}}
	}

	for _, tc := range []struct {
		name      string
		mutating  bool
		operation admissionv1.Operation
		volume    string
		// want is the denial message, or the tenant of the injected selector
		// for the mutating handler; "" allows.
		want string
	}{
		{name: "validating CREATE of a volume of the API Tenant", operation: admissionv1.Create, volume: "gas-volume"},
		{name: "validating CREATE of a volume of the cached Tenant", operation: admissionv1.Create, volume: "oil-volume", want: "cross-tenant mount for PersistentVolume oil-volume"},
		{name: "validating UPDATE of a volume of the API Tenant", operation: admissionv1.Update, volume: "gas-volume"},
		{name: "validating UPDATE of a volume of the cached Tenant", operation: admissionv1.Update, volume: "oil-volume", want: "cross-tenant mount for PersistentVolume oil-volume"},
		{name: "mutating CREATE selects the volumes of the API Tenant", mutating: true, operation: admissionv1.Create, volume: "oil-volume", want: "gas"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			cache, cacheReads := pvcTenantStore(t, scheme, oil, gas, namespace(oil), volume(oil), volume(gas))
			api, apiReads := pvcTenantStore(t, scheme, oil, gas, namespace(gas), volume(oil), volume(gas))

			handler := Handler(PersistentVolumeValidatingVolume(nil), PersistentVolumeValidatingClass())
			if tc.mutating {
				handler = MutatingHandler(PersistentVolumeMutatingVolume())
			}

			decoder := admission.NewDecoder(scheme)
			request := pvcAdmissionRequest(t, claim(tc.volume))
			fn := handler.OnCreate(cache, api, decoder, events.NewDiscardRecorder())

			if tc.operation == admissionv1.Update {
				request = pvcUpdateAdmissionRequest(t, claim(tc.volume), claim(tc.volume))
				fn = handler.OnUpdate(cache, api, decoder, events.NewDiscardRecorder())
			}

			response := fn(t.Context(), request)

			switch {
			case tc.mutating:
				if response == nil || !response.Allowed || len(response.Patches) == 0 {
					t.Fatalf("response = %#v, want a selector patch", response)
				}

				patched, err := json.Marshal(response.Patches)
				if err != nil {
					t.Fatal(err)
				}

				if !strings.Contains(string(patched), `"values":["`+tc.want+`"]`) {
					t.Fatalf("patches = %s, want the selector of Tenant %q", patched, tc.want)
				}
			case tc.want == "":
				if response != nil {
					t.Fatalf("response = %#v, want the volume of the API Tenant allowed", response)
				}
			default:
				if response == nil || response.Allowed || !strings.Contains(response.Result.Message, tc.want) {
					t.Fatalf("response = %#v, want a denial with %q", response, tc.want)
				}
			}

			if *cacheReads != 0 {
				t.Fatalf("cache reads = %d, want 0: the tenant check reads only the API", *cacheReads)
			}

			if *apiReads == 0 {
				t.Fatal("the API reader was not read")
			}
		})
	}
}
