// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package mutation

import (
	"encoding/json"
	"testing"

	jsonpatch "github.com/evanphx/json-patch/v5"
	admissionv1 "k8s.io/api/admission/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/meta"
)

func tenantMetadataRequest(t testing.TB, tnt *capsulev1beta2.Tenant) (admission.Request, admission.Decoder) {
	t.Helper()
	scheme := runtime.NewScheme()
	if err := capsulev1beta2.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(tnt)
	if err != nil {
		t.Fatal(err)
	}
	return admission.Request{Operation: admissionv1.Create, Object: runtime.RawExtension{Raw: raw}}, admission.NewDecoder(scheme)
}

func TestTenantAdmissionProtectsLifecycle(t *testing.T) {
	for _, mode := range []string{"create", "label-already-present", "update-repair", "already-protected", "deleting", "deleting-finalized", "dry-run"} {
		t.Run(mode, func(t *testing.T) {
			tnt := &capsulev1beta2.Tenant{Name: "tenant-a", Labels: map[string]string{"example.com/keep": "yes"}, Finalizers: []string{"example.com/other"}}
			switch mode {
			case "label-already-present", "already-protected":
				tnt.Labels[meta.TenantNameLabel] = tnt.Name
			}
			if mode == "already-protected" || mode == "deleting" {
				tnt.Finalizers = append(tnt.Finalizers, meta.ControllerFinalizer)
			}
			if mode == "deleting" || mode == "deleting-finalized" {
				now := metav1.Now()
				tnt.DeletionTimestamp = &now
			}
			req, decoder := tenantMetadataRequest(t, tnt)
			if mode == "dry-run" {
				yes := true
				req.DryRun = &yes
			}
			handler := MetaHandler().OnCreate(nil, nil, decoder, nil)
			if mode == "update-repair" || tnt.DeletionTimestamp != nil {
				req.Operation = admissionv1.Update
				handler = MetaHandler().OnUpdate(nil, nil, decoder, nil)
			}
			response := handler(t.Context(), req)
			raw := req.Object.Raw
			if response != nil {
				if !response.Allowed {
					t.Fatalf("unexpected denial: %v", response)
				}
				patchRaw, err := json.Marshal(response.Patches)
				if err != nil {
					t.Fatal(err)
				}
				patch, err := jsonpatch.DecodePatch(patchRaw)
				if err != nil {
					t.Fatal(err)
				}
				raw, err = patch.Apply(raw)
				if err != nil {
					t.Fatal(err)
				}
			}
			result := &capsulev1beta2.Tenant{}
			if err := json.Unmarshal(raw, result); err != nil {
				t.Fatal(err)
			}
			if got := controllerutil.ContainsFinalizer(result, meta.ControllerFinalizer); got != (mode != "deleting-finalized") {
				t.Fatalf("finalizers=%v", result.Finalizers)
			}
			if !controllerutil.ContainsFinalizer(result, "example.com/other") || result.Labels["example.com/keep"] != "yes" || result.Labels[meta.TenantNameLabel] != result.Name {
				t.Fatalf("metadata lost: %v", result.ObjectMeta)
			}
			if mode == "already-protected" && response != nil {
				t.Fatal("unchanged Tenant should not be patched")
			}
		})
	}
}

func BenchmarkTenantLifecycleAdmission(b *testing.B) {
	for _, mode := range []string{"create", "unchanged", "deleting"} {
		b.Run(mode, func(b *testing.B) {
			tnt := &capsulev1beta2.Tenant{Name: "tenant-a"}
			if mode == "unchanged" {
				tnt.Labels = map[string]string{meta.TenantNameLabel: tnt.Name}
				tnt.Finalizers = []string{meta.ControllerFinalizer}
			}
			if mode == "deleting" {
				now := metav1.Now()
				tnt.DeletionTimestamp = &now
			}
			req, decoder := tenantMetadataRequest(b, tnt)
			handler := MetaHandler().OnCreate(nil, nil, decoder, nil)
			b.ReportAllocs()
			for b.Loop() {
				response := handler(b.Context(), req)
				if response != nil && !response.Allowed {
					b.Fatal(response)
				}
				if mode == "unchanged" && response != nil {
					b.Fatal("unnecessary patch")
				}
			}
		})
	}
}
