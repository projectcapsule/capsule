// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package pvc

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/runtime/handlers"
)

func BenchmarkPVCAdmission(b *testing.B) {
	for _, tenants := range []int{1, 100} {
		for _, requirements := range []int{1, 32} {
			for _, operation := range []string{"mutating-create", "mutating-update", "validating-update-allow", "validating-update-deny"} {
				b.Run(fmt.Sprintf("%s/tenants=%d/requirements=%d", operation, tenants, requirements), func(b *testing.B) {
					scheme := runtime.NewScheme()
					if err := corev1.AddToScheme(scheme); err != nil {
						b.Fatal(err)
					}
					if err := capsulev1beta2.AddToScheme(scheme); err != nil {
						b.Fatal(err)
					}
					objects := make([]client.Object, 0, tenants+2)
					for i := range tenants {
						objects = append(objects, &capsulev1beta2.Tenant{ObjectMeta: metav1.ObjectMeta{
							Name: fmt.Sprintf("tenant-%d", i), UID: types.UID(fmt.Sprintf("uid-%d", i)),
						}})
					}
					objects = append(objects, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{
						Name: "solar", OwnerReferences: []metav1.OwnerReference{{
							APIVersion: capsulev1beta2.GroupVersion.String(), Kind: "Tenant", Name: "tenant-0", UID: "uid-0",
						}},
					}})
					volumeTenant := "tenant-0"
					if operation == "validating-update-deny" {
						volumeTenant = "other-tenant"
					}
					objects = append(objects, &corev1.PersistentVolume{ObjectMeta: metav1.ObjectMeta{
						Name: "restored-pv", Labels: map[string]string{meta.TenantLabel: volumeTenant},
					}})
					reader := &pvcCountingReader{Reader: fake.NewClientBuilder().WithScheme(scheme).WithObjects(objects...).Build()}
					oldPVC := restoredPVCForUpdate()
					oldPVC.Spec.Selector = addTenantSelectorExpression(oldPVC.Spec.Selector, "tenant-0")
					for i := range requirements {
						oldPVC.Spec.Selector.MatchLabels[fmt.Sprintf("label-%d", i)] = "value"
					}
					newPVC := oldPVC.DeepCopy()
					newPVC.Spec.VolumeName = "restored-pv"
					newRaw, err := json.Marshal(newPVC)
					if err != nil {
						b.Fatal(err)
					}
					oldRaw, err := json.Marshal(oldPVC)
					if err != nil {
						b.Fatal(err)
					}
					request := admission.Request{AdmissionRequest: admissionv1.AdmissionRequest{
						Operation: admissionv1.Update, Namespace: "solar",
						Object: runtime.RawExtension{Raw: newRaw}, OldObject: runtime.RawExtension{Raw: oldRaw},
					}}
					decoder := admission.NewDecoder(scheme)
					var handler handlers.Func
					switch operation {
					case "mutating-create":
						request.Operation = admissionv1.Create
						handler = MutatingHandler(PersistentVolumeMutatingVolume()).OnCreate(nil, reader, decoder, nil)
					case "mutating-update":
						handler = MutatingHandler(PersistentVolumeMutatingVolume()).OnUpdate(nil, reader, decoder, nil)
					default:
						handler = Handler(PersistentVolumeValidatingVolume()).OnUpdate(nil, reader, decoder, nil)
					}
					ctx := context.Background()
					b.ReportAllocs()
					b.ResetTimer()
					for b.Loop() {
						response := handler(ctx, request)
						if operation == "validating-update-deny" {
							if response == nil || response.Allowed {
								b.Fatal("cross-tenant mount was not denied")
							}
						} else if response != nil && !response.Allowed {
							b.Fatalf("unexpected denial: %#v", response)
						}
					}
					b.ReportMetric(float64(reader.gets)/float64(b.N), "gets/op")
				})
			}
		}
	}
}
