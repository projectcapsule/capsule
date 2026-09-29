// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package tls

import (
	"fmt"
	"testing"

	admissionv1 "k8s.io/api/admissionregistration/v1"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	mockclient "github.com/projectcapsule/capsule/internal/mocks/client"
	"github.com/projectcapsule/capsule/pkg/runtime/configuration"
)

func BenchmarkControllerTLS(b *testing.B) {
	for _, count := range []int{1, 32} {
		b.Run(fmt.Sprintf("valid-certificate/webhooks=%d", count), func(b *testing.B) {
			mutating := &admissionv1.MutatingWebhookConfiguration{Name: testMutatingConfiguration}
			validating := &admissionv1.ValidatingWebhookConfiguration{Name: testValidatingConfiguration}
			for i := range count {
				name := fmt.Sprintf("hook-%d.example.com", i)
				mutating.Webhooks = append(mutating.Webhooks, admissionv1.MutatingWebhook{Name: name})
				validating.Webhooks = append(validating.Webhooks, admissionv1.ValidatingWebhook{Name: name})
			}
			r, base := newTestTLSReconciler(b, mutating, validating)
			calls := &mockclient.CallCounter{}
			c := interceptor.NewClient(base.(client.WithWatch), calls.Interceptors())
			r.Client = c
			r.Configuration = configuration.NewCapsuleConfiguration(b.Context(), c, c, nil, "capsule")
			req := reconcile.Request{Namespace: testNamespace, Name: testSecretName}
			if _, err := r.Reconcile(b.Context(), req); err != nil {
				b.Fatal(err)
			}
			secret := &corev1.Secret{}
			if err := c.Get(b.Context(), req.NamespacedName, secret); err != nil {
				b.Fatal(err)
			}
			if len(secret.Data[corev1.TLSCertKey]) == 0 {
				b.Fatal("missing certificate")
			}
			calls.Reset()
			b.ReportAllocs()
			for b.Loop() {
				result, err := r.Reconcile(b.Context(), req)
				if err != nil {
					b.Fatal(err)
				}
				if result.RequeueAfter <= 0 {
					b.Fatal("valid certificate did not schedule renewal")
				}
			}
			calls.Report(b)
		})
	}
}
