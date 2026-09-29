// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package mock_client

import (
	"context"
	"sync/atomic"
	"testing"

	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
)

// CallCounter measures client operations, including failed requests and status writes.
// It measures calls to the injected client, not network round trips or cache misses.
type CallCounter struct{ gets, lists, writes atomic.Int64 }

func (c *CallCounter) Reset() {
	c.gets.Store(0)
	c.lists.Store(0)
	c.writes.Store(0)
}

func (c *CallCounter) Report(b *testing.B) {
	b.Helper()
	b.ReportMetric(float64(c.gets.Load())/float64(b.N), "GET/op")
	b.ReportMetric(float64(c.lists.Load())/float64(b.N), "LIST/op")
	b.ReportMetric(float64(c.writes.Load())/float64(b.N), "write/op")
}

func (c *CallCounter) Interceptors() interceptor.Funcs {
	return interceptor.Funcs{
		Get: func(ctx context.Context, cl client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
			c.gets.Add(1)

			return cl.Get(ctx, key, obj, opts...)
		},
		List: func(ctx context.Context, cl client.WithWatch, list client.ObjectList, opts ...client.ListOption) error {
			c.lists.Add(1)

			return cl.List(ctx, list, opts...)
		},
		Create: func(ctx context.Context, cl client.WithWatch, obj client.Object, opts ...client.CreateOption) error {
			c.writes.Add(1)

			return cl.Create(ctx, obj, opts...)
		},
		Update: func(ctx context.Context, cl client.WithWatch, obj client.Object, opts ...client.UpdateOption) error {
			c.writes.Add(1)

			return cl.Update(ctx, obj, opts...)
		},
		Patch: func(ctx context.Context, cl client.WithWatch, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
			c.writes.Add(1)

			return cl.Patch(ctx, obj, patch, opts...)
		},
		Delete: func(ctx context.Context, cl client.WithWatch, obj client.Object, opts ...client.DeleteOption) error {
			c.writes.Add(1)

			return cl.Delete(ctx, obj, opts...)
		},
		DeleteAllOf: func(ctx context.Context, cl client.WithWatch, obj client.Object, opts ...client.DeleteAllOfOption) error {
			c.writes.Add(1)

			return cl.DeleteAllOf(ctx, obj, opts...)
		},
		Apply: func(ctx context.Context, cl client.WithWatch, obj runtime.ApplyConfiguration, opts ...client.ApplyOption) error {
			c.writes.Add(1)

			return cl.Apply(ctx, obj, opts...)
		},
		SubResourceGet: func(ctx context.Context, cl client.Client, name string, obj, sub client.Object, opts ...client.SubResourceGetOption) error {
			c.gets.Add(1)

			return cl.SubResource(name).Get(ctx, obj, sub, opts...)
		},
		SubResourceCreate: func(ctx context.Context, cl client.Client, name string, obj, sub client.Object, opts ...client.SubResourceCreateOption) error {
			c.writes.Add(1)

			return cl.SubResource(name).Create(ctx, obj, sub, opts...)
		},
		SubResourceUpdate: func(ctx context.Context, cl client.Client, name string, obj client.Object, opts ...client.SubResourceUpdateOption) error {
			c.writes.Add(1)

			return cl.SubResource(name).Update(ctx, obj, opts...)
		},
		SubResourcePatch: func(ctx context.Context, cl client.Client, name string, obj client.Object, patch client.Patch, opts ...client.SubResourcePatchOption) error {
			c.writes.Add(1)

			return cl.SubResource(name).Patch(ctx, obj, patch, opts...)
		},
		SubResourceApply: func(ctx context.Context, cl client.Client, name string, obj runtime.ApplyConfiguration, opts ...client.SubResourceApplyOption) error {
			c.writes.Add(1)

			return cl.SubResource(name).Apply(ctx, obj, opts...)
		},
	}
}
