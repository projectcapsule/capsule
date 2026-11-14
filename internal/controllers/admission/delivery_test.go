// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package admission

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
)

func TestTriggerDeliveryKeySharedAcrossReplicas(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := capsulev1beta2.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	c := fake.NewClientBuilder().WithScheme(scheme).Build()
	owner := &capsulev1beta2.CapsuleConfiguration{Name: "default", UID: "current"}
	const replicas = 16
	results := make(chan string, replicas)
	var wg sync.WaitGroup
	for range replicas {
		wg.Go(func() {
			key, err := LoadTriggerDeliveryKey(t.Context(), c, "capsule-system", owner)
			if err != nil {
				t.Error(err)
				return
			}
			results <- key.Path(strings.Repeat("a", 64))
		})
	}
	wg.Wait()
	close(results)
	first := ""
	for path := range results {
		if first != "" && path != first {
			t.Fatal("replicas disagree on delivery key")
		}
		first = path
	}
	list := &corev1.SecretList{}
	if err := c.List(t.Context(), list); err != nil {
		t.Fatal(err)
	}
	if len(list.Items) != 1 || len(list.Items[0].Data["key"]) != 32 {
		t.Fatal("did not persist exactly one key")
	}
	secret := list.Items[0].DeepCopy()
	for _, tc := range []struct {
		name   string
		mutate func(*corev1.Secret)
	}{
		{"missing-owner", func(s *corev1.Secret) { s.OwnerReferences = nil }},
		{"stale-owner", func(s *corev1.Secret) { s.OwnerReferences[0].UID = "old" }},
		{"mutable", func(s *corev1.Secret) { s.Immutable = nil }},
		{"wrong-type", func(s *corev1.Secret) { s.Type = corev1.SecretTypeTLS }},
		{"invalid-key", func(s *corev1.Secret) { s.Data["key"] = []byte("bad") }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			invalid := secret.DeepCopy()
			tc.mutate(invalid)
			broken := fake.NewClientBuilder().WithScheme(scheme).WithObjects(invalid).Build()
			if _, err := LoadTriggerDeliveryKey(t.Context(), broken, "capsule-system", owner); err == nil {
				t.Fatal("accepted invalid delivery key")
			}
		})
	}
	failure := errors.New("unavailable")
	failing := fake.NewClientBuilder().WithScheme(scheme).WithInterceptorFuncs(interceptor.Funcs{
		Get: func(context.Context, client.WithWatch, client.ObjectKey, client.Object, ...client.GetOption) error {
			return failure
		},
	}).Build()
	if _, err := LoadTriggerDeliveryKey(t.Context(), failing, "capsule-system", owner); !errors.Is(err, failure) {
		t.Fatalf("lost read error: %v", err)
	}
	failing = fake.NewClientBuilder().WithScheme(scheme).WithInterceptorFuncs(interceptor.Funcs{
		Create: func(context.Context, client.WithWatch, client.Object, ...client.CreateOption) error { return failure },
	}).Build()
	if _, err := LoadTriggerDeliveryKey(t.Context(), failing, "capsule-system", owner); !errors.Is(err, failure) {
		t.Fatalf("lost create error: %v", err)
	}
	if _, err := LoadTriggerDeliveryKey(t.Context(), c, "", owner); err == nil {
		t.Fatal("accepted missing namespace")
	}
	if _, err := LoadTriggerDeliveryKey(t.Context(), c, "capsule-system", nil); err == nil {
		t.Fatal("accepted missing configuration")
	}
}
