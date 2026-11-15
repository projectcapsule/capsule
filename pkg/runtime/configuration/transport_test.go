// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package configuration_test

import (
	"context"
	"errors"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/runtime/configuration"
)

func TestServiceAccountTransportSnapshotPreservesBaseAndContext(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	secret := &corev1.Secret{Name: "ca", Namespace: "controller", Data: map[string][]byte{"ca.crt": []byte("pinned-ca")}}
	reads := 0
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(secret).WithInterceptorFuncs(interceptor.Funcs{Get: func(ctx context.Context, inner client.WithWatch, key client.ObjectKey, object client.Object, opts ...client.GetOption) error {
		reads++
		if err := ctx.Err(); err != nil {
			return err
		}
		return inner.Get(ctx, key, object, opts...)
	}}).Build()
	base := &rest.Config{Host: "https://base.example", BearerTokenFile: "/projected/token", CAData: []byte("base-ca"), CAFile: "/base/ca"}
	props := capsulev1beta2.ServiceAccountClient{Endpoint: "https://pinned.example", CASecretName: "ca", CASecretNamespace: "controller", CASecretKey: "ca.crt"}
	got, err := configuration.ServiceAccountRESTConfig(t.Context(), c, base, props)
	if err != nil || got.Host != props.Endpoint || string(got.CAData) != "pinned-ca" || got.CAFile != "" || got.BearerTokenFile != base.BearerTokenFile || reads != 1 {
		t.Fatalf("pinned transport: cfg=%v reads=%d err=%v", got, reads, err)
	}
	if base.Host != "https://base.example" || string(base.CAData) != "base-ca" || base.CAFile != "/base/ca" || base.Insecure {
		t.Fatal("transport construction mutated shared base configuration")
	}
	props.SkipTLSVerify = true
	got, err = configuration.ServiceAccountRESTConfig(t.Context(), nil, base, props)
	if err != nil || !got.Insecure || got.CAData != nil || got.CAFile != "" || reads != 1 {
		t.Fatal("explicit TLS skip read a CA or retained trust settings")
	}
	props.SkipTLSVerify = false
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := configuration.ServiceAccountRESTConfig(ctx, c, base, props); !errors.Is(err, context.Canceled) {
		t.Fatalf("CA loading lost cancellation: %v", err)
	}
	if _, err := configuration.ServiceAccountRESTConfig(t.Context(), nil, base, props); err == nil {
		t.Fatal("missing CA reader silently accepted")
	}
	if _, err := configuration.ServiceAccountRESTConfig(t.Context(), c, nil, props); err == nil {
		t.Fatal("missing base transport silently accepted")
	}
}
