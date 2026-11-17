// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package users

import (
	"testing"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/projectcapsule/capsule/pkg/api/meta"
)

func TestImpersonatedClientSupportsWatchWithoutChangingBaseIdentity(t *testing.T) {
	base := &rest.Config{Host: "https://unused.example", Impersonate: rest.ImpersonationConfig{UserName: "base"}}
	object, err := ImpersonatedKubernetesClientForServiceAccount(base, runtime.NewScheme(), meta.NamespacedRFC1123ObjectReferenceWithNamespace{Name: "reader", Namespace: "a"})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := object.(client.WithWatch); !ok {
		t.Fatal("execution client does not support watches")
	}
	if base.Impersonate.UserName != "base" {
		t.Fatal("client construction mutated base credentials")
	}
}

func BenchmarkImpersonatedClientConstruction(b *testing.B) {
	base := &rest.Config{Host: "https://unused.example"}
	scheme := runtime.NewScheme()
	account := meta.NamespacedRFC1123ObjectReferenceWithNamespace{Name: "reader", Namespace: "a"}
	b.ReportAllocs()
	for b.Loop() {
		if _, err := ImpersonatedKubernetesClientForServiceAccount(base, scheme, account); err != nil {
			b.Fatal(err)
		}
	}
}
