// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package admission

import (
	"fmt"
	"testing"

	"github.com/go-logr/logr"
	admissionv1 "k8s.io/api/admissionregistration/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	mockclient "github.com/projectcapsule/capsule/internal/mocks/client"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	admissionapi "github.com/projectcapsule/capsule/pkg/runtime/admission"
	"github.com/projectcapsule/capsule/pkg/runtime/configuration"
)

func BenchmarkControllerAdmission(b *testing.B) {
	for _, kind := range []string{"mutating", "validating"} {
		for _, count := range []int{1, 32} {
			b.Run(fmt.Sprintf("%s/steady/webhooks=%d", kind, count), func(b *testing.B) {
				scheme := runtime.NewScheme()
				for _, add := range []func(*runtime.Scheme) error{admissionv1.AddToScheme, capsulev1beta2.AddToScheme} {
					if err := add(scheme); err != nil {
						b.Fatal(err)
					}
				}
				url := "https://capsule.example.com"
				sideEffects := admissionv1.SideEffectClassNone
				dyn := admissionapi.DynamicAdmissionConfig{Name: meta.RFC1123Name(kind), Client: &admissionv1.WebhookClientConfig{URL: &url, CABundle: []byte("fixture-ca")}}
				cfg := &capsulev1beta2.CapsuleConfiguration{
					Name: "capsule",
					UID:  "config-uid",
					Spec: capsulev1beta2.CapsuleConfigurationSpec{
						Admission: capsulev1beta2.DynamicAdmission{
							Mutating:   &capsulev1beta2.DynamicMutatingAdmissionConfig{DynamicAdmissionConfig: dyn},
							Validating: &capsulev1beta2.DynamicValidatingAdmissionConfig{DynamicAdmissionConfig: dyn},
						},
					},
				}
				for i := range count {
					name := fmt.Sprintf("hook-%d.capsule.example.com", i)
					cfg.Spec.Admission.Mutating.Webhooks = append(cfg.Spec.Admission.Mutating.Webhooks, &admissionapi.MutatingWebhook{Name: name, SideEffects: &sideEffects, AdmissionReviewVersions: []string{"v1"}})
					cfg.Spec.Admission.Validating.Webhooks = append(cfg.Spec.Admission.Validating.Webhooks, &admissionapi.ValidatingWebhook{Name: name, SideEffects: &sideEffects, AdmissionReviewVersions: []string{"v1"}})
				}
				calls := &mockclient.CallCounter{}
				c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(cfg).WithInterceptorFuncs(calls.Interceptors()).Build()
				configuration := configuration.NewCapsuleConfiguration(b.Context(), c, c, nil, cfg.Name)
				var r reconcile.Reconciler = &mutatingReconciler{client: c, configuration: configuration, log: logr.Discard()}
				if kind == "validating" {
					r = &validatingReconciler{client: c, configuration: configuration, log: logr.Discard()}
				}
				req := reconcile.Request{Name: kind}
				if _, err := r.Reconcile(b.Context(), req); err != nil {
					b.Fatal(err)
				}
				if kind == "mutating" {
					obj := &admissionv1.MutatingWebhookConfiguration{}
					if err := c.Get(b.Context(), client.ObjectKey{Name: kind}, obj); err != nil {
						b.Fatal(err)
					}
					if len(obj.Webhooks) != count {
						b.Fatal("missing webhooks")
					}
				} else {
					obj := &admissionv1.ValidatingWebhookConfiguration{}
					if err := c.Get(b.Context(), client.ObjectKey{Name: kind}, obj); err != nil {
						b.Fatal(err)
					}
					if len(obj.Webhooks) != count {
						b.Fatal("missing webhooks")
					}
				}
				calls.Reset()
				b.ReportAllocs()
				for b.Loop() {
					if _, err := r.Reconcile(b.Context(), req); err != nil {
						b.Fatal(err)
					}
				}
				calls.Report(b)
			})
		}
	}
}
