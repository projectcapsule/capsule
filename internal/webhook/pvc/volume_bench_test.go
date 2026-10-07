// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package pvc

import (
	"encoding/json"
	"testing"

	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsule "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/meta"
)

func BenchmarkPVCVolumeBaseline(b *testing.B) {
	for _, mode := range []string{"same-tenant", "other-tenant", "unlabeled", "dynamic", "bound"} {
		b.Run(mode, func(b *testing.B) {
			scheme := runtime.NewScheme()
			if err := corev1.AddToScheme(scheme); err != nil {
				b.Fatal(err)
			}
			if err := capsule.AddToScheme(scheme); err != nil {
				b.Fatal(err)
			}
			tenant := &capsule.Tenant{Name: "tenant-a", UID: "tenant-a"}
			ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "destination", OwnerReferences: []metav1.OwnerReference{{APIVersion: capsule.GroupVersion.String(), Kind: "Tenant", Name: tenant.Name, UID: tenant.UID}}}}
			pv := &corev1.PersistentVolume{ObjectMeta: metav1.ObjectMeta{Name: "volume", Labels: map[string]string{meta.TenantLabel: tenant.Name}}}
			old := &corev1.PersistentVolumeClaim{ObjectMeta: metav1.ObjectMeta{Name: "claim", Namespace: ns.Name}}
			claim := old.DeepCopy()
			claim.Spec.VolumeName = pv.Name
			switch mode {
			case "other-tenant":
				pv.Labels[meta.TenantLabel] = "other"
			case "unlabeled":
				pv.Labels = nil
			case "dynamic":
				claim.Spec.VolumeName = ""
			case "bound":
				old.Status.Phase = corev1.ClaimBound
				old.Spec.VolumeName = pv.Name
			}
			c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(tenant, ns, pv).Build()
			req := admission.Request{AdmissionRequest: admissionv1.AdmissionRequest{Operation: admissionv1.Update, Namespace: ns.Name}}
			var err error
			req.Object.Raw, err = json.Marshal(claim)
			if err != nil {
				b.Fatal(err)
			}
			req.OldObject.Raw, err = json.Marshal(old)
			if err != nil {
				b.Fatal(err)
			}
			handle := Handler(PersistentVolumeValidatingVolume(nil)).OnUpdate(c, c, admission.NewDecoder(scheme), nil)
			b.ReportAllocs()
			for b.Loop() {
				response := handle(b.Context(), req)
				wantAllow := mode != "other-tenant" && mode != "unlabeled"
				if (response == nil) != wantAllow {
					b.Fatalf("unexpected response: %#v", response)
				}
			}
		})
	}
}
