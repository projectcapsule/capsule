// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package processor

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	mockclient "github.com/projectcapsule/capsule/internal/mocks/client"
	"github.com/projectcapsule/capsule/pkg/api/meta"
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

func BenchmarkApplyReplicationMetadataConflict(b *testing.B) {
	for _, tenants := range []int{1, 128} {
		b.Run(fmt.Sprintf("tenants=%d", tenants), func(b *testing.B) {
			calls := &mockclient.CallCounter{}
			scheme := runtime.NewScheme()
			if err := capsulev1beta2.AddToScheme(scheme); err != nil {
				b.Fatal(err)
			}
			objects := make([]client.Object, 0, tenants)
			for i := range tenants {
				objects = append(objects, &capsulev1beta2.Tenant{Name: fmt.Sprintf("tenant-%d", i)})
			}
			base := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objects...).WithReturnManagedFields().Build()
			needsConflict := false
			racing := interceptor.NewClient(base, interceptor.Funcs{
				Patch: func(ctx context.Context, c client.WithWatch, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
					manager := (&client.PatchOptions{}).ApplyOptions(opts).FieldManager
					if needsConflict && patch.Type() == types.JSONPatchType && manager == meta.ResourceControllerFieldOwnerPrefix() {
						needsConflict = false
						current := policyConfigMap(obj.GetNamespace(), obj.GetName())
						if err := c.Get(ctx, client.ObjectKeyFromObject(obj), current); err != nil {
							return err
						}
						current.SetAnnotations(map[string]string{"external.example/change": "retained"})
						if err := c.Update(ctx, current); err != nil {
							return err
						}
					}
					return c.Patch(ctx, obj, patch, opts...)
				},
			})
			c := interceptor.NewClient(racing, calls.Interceptors())
			p := &Processor{}
			obj := policyConfigMap("tenant-a", "created")
			b.ReportAllocs()
			for b.Loop() {
				needsConflict = true
				result, err := p.Apply(b.Context(), c, obj, "replication/test", false, false, nil, nil, nil)
				if err != nil || !result.Created || needsConflict {
					b.Fatalf("retry metadata: %+v, %v", result, err)
				}
				b.StopTimer()
				if err := base.Delete(b.Context(), obj); err != nil {
					b.Fatal(err)
				}
				b.StartTimer()
			}
			calls.Report(b)
		})
	}
}

func BenchmarkApplyReplicationReadFailure(b *testing.B) {
	calls := &mockclient.CallCounter{}
	base := fake.NewClientBuilder().WithReturnManagedFields().Build()
	written := false
	created := metav1.NewTime(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	failing := interceptor.NewClient(base, interceptor.Funcs{
		Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
			if written {
				return apierrors.NewServiceUnavailable("target read unavailable")
			}
			return c.Get(ctx, key, obj, opts...)
		},
		Create: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.CreateOption) error {
			obj.SetUID("created-uid")
			obj.SetCreationTimestamp(created)
			return c.Create(ctx, obj, opts...)
		},
		Patch: func(ctx context.Context, c client.WithWatch, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
			if patch.Type() == types.ApplyPatchType {
				written = true
				return apierrors.NewConflict(corev1.Resource("configmaps"), obj.GetName(), errors.New("concurrent writer"))
			}
			return c.Patch(ctx, obj, patch, opts...)
		},
	})
	c := interceptor.NewClient(failing, calls.Interceptors())
	p := &Processor{}
	obj := policyConfigMap("tenant-a", "created")
	b.ReportAllocs()
	for b.Loop() {
		written = false
		result, err := p.Apply(b.Context(), c, obj, "replication/test", false, false, nil, nil, nil)
		if !apierrors.IsServiceUnavailable(err) || !result.Created || result.UID != "created-uid" || result.LastApply == nil || !result.LastApply.Equal(&created) {
			b.Fatalf("lost confirmed creation: %+v, %v", result, err)
		}
		b.StopTimer()
		if err := base.Delete(b.Context(), obj); err != nil {
			b.Fatal(err)
		}
		b.StartTimer()
	}
	calls.Report(b)
}
