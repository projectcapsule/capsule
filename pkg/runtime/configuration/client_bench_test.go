// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package configuration_test

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	mockclient "github.com/projectcapsule/capsule/internal/mocks/client"
	"github.com/projectcapsule/capsule/pkg/runtime/configuration"
)

func BenchmarkServiceAccountTransportConfiguration(b *testing.B) {
	for _, mode := range []string{"default", "endpoint", "skip-tls", "ca-secret"} {
		b.Run(mode, func(b *testing.B) {
			scheme := runtime.NewScheme()
			_ = corev1.AddToScheme(scheme)
			_ = capsulev1beta2.AddToScheme(scheme)
			object := &capsulev1beta2.CapsuleConfiguration{Name: "default"}
			switch mode {
			case "endpoint":
				object.Spec.Impersonation.Endpoint = "https://pinned.example"
			case "skip-tls":
				object.Spec.Impersonation.SkipTLSVerify = true
			case "ca-secret":
				object.Spec.Impersonation.CASecretName = "ca"
				object.Spec.Impersonation.CASecretNamespace = "controller"
				object.Spec.Impersonation.CASecretKey = "ca.crt"
			}
			calls := &mockclient.CallCounter{}
			c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(object, &corev1.Secret{Name: "ca", Namespace: "controller", Data: map[string][]byte{"ca.crt": []byte("ca-data")}}).WithInterceptorFuncs(calls.Interceptors()).Build()
			cfg := configuration.NewCapsuleConfiguration(b.Context(), c, c, &rest.Config{Host: "https://base.example"}, object.Name)
			calls.Reset()
			b.ReportAllocs()
			for b.Loop() {
				if _, err := cfg.ServiceAccountClient(b.Context()); err != nil {
					b.Fatal(err)
				}
			}
			calls.Report(b)
		})
	}
}
