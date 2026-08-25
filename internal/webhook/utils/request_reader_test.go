// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package utils

import (
	"context"
	"sync"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestRequestCachingReaderDeduplicatesAndIsolatesGets(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}

	base := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(&corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name:   "solar",
				Labels: map[string]string{"original": "true"},
			},
		}).
		Build()
	counting := &countingReader{Reader: base}
	reader := NewRequestCachingReader(counting)

	first := &corev1.Namespace{}
	if err := reader.Get(context.Background(), client.ObjectKey{Name: "solar"}, first); err != nil {
		t.Fatal(err)
	}

	first.Labels["mutated"] = "true"

	second := &corev1.Namespace{}
	if err := reader.Get(context.Background(), client.ObjectKey{Name: "solar"}, second); err != nil {
		t.Fatal(err)
	}

	if counting.gets != 1 {
		t.Fatalf("underlying gets = %d, want 1", counting.gets)
	}

	if second.Labels["original"] != "true" || second.Labels["mutated"] != "" {
		t.Fatalf("cached object was not isolated: %#v", second.Labels)
	}
}

func TestRequestCachingReaderSeparatesObjectTypes(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}

	base := fake.NewClientBuilder().WithScheme(scheme).Build()
	counting := &countingReader{Reader: base}
	reader := NewRequestCachingReader(counting)
	key := client.ObjectKey{Name: "missing"}

	_ = reader.Get(context.Background(), key, &corev1.Namespace{})
	_ = reader.Get(context.Background(), key, &corev1.ConfigMap{})

	if counting.gets != 2 {
		t.Fatalf("underlying gets = %d, want 2 for different object types", counting.gets)
	}
}

func TestRequestCachingReaderDeduplicatesConcurrentGets(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}

	base := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(&corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{Name: "solar"},
		}).
		Build()
	counting := &countingReader{Reader: base}
	reader := NewRequestCachingReader(counting)

	start := make(chan struct{})
	errors := make(chan error, 2)

	var wg sync.WaitGroup
	for range 2 {
		wg.Add(1)

		go func() {
			defer wg.Done()
			<-start

			errors <- reader.Get(
				context.Background(),
				client.ObjectKey{Name: "solar"},
				&corev1.Namespace{},
			)
		}()
	}

	close(start)
	wg.Wait()
	close(errors)

	for err := range errors {
		if err != nil {
			t.Fatal(err)
		}
	}

	if counting.gets != 1 {
		t.Fatalf("underlying gets = %d, want 1", counting.gets)
	}
}
