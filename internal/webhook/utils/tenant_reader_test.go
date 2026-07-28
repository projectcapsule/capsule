// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package utils

import (
	"context"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
)

type countingReader struct {
	client.Reader

	gets int
}

func (r *countingReader) Get(
	ctx context.Context,
	key client.ObjectKey,
	obj client.Object,
	opts ...client.GetOption,
) error {
	r.gets++

	return r.Reader.Get(ctx, key, obj, opts...)
}

func TestTenantCachingReaderDeduplicatesTenantGets(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	if err := capsulev1beta2.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}

	base := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(&capsulev1beta2.Tenant{
			ObjectMeta: metav1.ObjectMeta{Name: "solar"},
		}).
		Build()
	counting := &countingReader{Reader: base}
	reader := NewTenantCachingReader(counting)

	first := &capsulev1beta2.Tenant{}
	if err := reader.Get(context.Background(), client.ObjectKey{Name: "solar"}, first); err != nil {
		t.Fatal(err)
	}

	first.Labels = map[string]string{"mutated": "true"}

	second := &capsulev1beta2.Tenant{}
	if err := reader.Get(context.Background(), client.ObjectKey{Name: "solar"}, second); err != nil {
		t.Fatal(err)
	}

	if counting.gets != 1 {
		t.Fatalf("underlying Tenant gets = %d, want 1", counting.gets)
	}

	if second.Labels["mutated"] != "" {
		t.Fatal("cached Tenant result was not deep-copied")
	}
}
