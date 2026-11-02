// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package processor

import (
	"context"
	"errors"
	"testing"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	mockclient "github.com/projectcapsule/capsule/internal/mocks/client"
)

func BenchmarkApplyReplicationCreation(b *testing.B) {
	calls := &mockclient.CallCounter{}
	base := fake.NewClientBuilder().WithReturnManagedFields().Build()
	c := interceptor.NewClient(base, calls.Interceptors())
	p := &Processor{}
	obj := policyConfigMap("tenant-a", "created")
	b.ReportAllocs()
	for b.Loop() {
		result, err := p.Apply(b.Context(), c, obj, "replication/test", false, false, nil, nil, nil)
		if err != nil || !result.Created {
			b.Fatalf("apply: %+v, %v", result, err)
		}
		b.StopTimer()
		if err := base.Delete(b.Context(), obj); client.IgnoreNotFound(err) != nil {
			b.Fatal(err)
		}
		b.StartTimer()
	}
	calls.Report(b)
}

func BenchmarkApplyReplicationConflict(b *testing.B) {
	calls := &mockclient.CallCounter{}
	base := fake.NewClientBuilder().WithReturnManagedFields().Build()
	needsConflict := false
	conflicting := interceptor.NewClient(base, interceptor.Funcs{
		Patch: func(ctx context.Context, c client.WithWatch, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
			if needsConflict && patch.Type() == types.ApplyPatchType {
				needsConflict = false
				return apierrors.NewConflict(corev1.Resource("configmaps"), obj.GetName(), errors.New("concurrent writer"))
			}
			return c.Patch(ctx, obj, patch, opts...)
		},
	})
	c := interceptor.NewClient(conflicting, calls.Interceptors())
	p := &Processor{}
	obj := policyConfigMap("tenant-a", "created")
	b.ReportAllocs()
	for b.Loop() {
		needsConflict = true
		result, err := p.Apply(b.Context(), c, obj, "replication/test", false, false, nil, nil, nil)
		if err != nil || !result.Created || needsConflict {
			b.Fatalf("retry: %+v, %v", result, err)
		}
		b.StopTimer()
		if err := base.Delete(b.Context(), obj); err != nil {
			b.Fatal(err)
		}
		b.StartTimer()
	}
	calls.Report(b)
}
