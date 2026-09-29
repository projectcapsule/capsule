// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package pv

import (
	"fmt"
	"testing"

	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	mockclient "github.com/projectcapsule/capsule/internal/mocks/client"
	"github.com/projectcapsule/capsule/pkg/api/meta"
)

func BenchmarkControllerPersistentVolume(b *testing.B) {
	for _, tenants := range []int{1, 32} {
		b.Run(fmt.Sprintf("steady/tenants=%d", tenants), func(b *testing.B) {
			scheme := persistentVolumeTestScheme(b)
			objects := []client.Object{}
			for i := range tenants {
				tnt := persistentVolumeTestTenant()
				tnt.Name = fmt.Sprintf("tenant-%d", i)
				ns := persistentVolumeTestNamespace(tnt)
				ns.Name = tnt.Name
				pv := persistentVolumeTestVolume(ns.Name, nil)
				objects = append(objects, tnt, ns, pv)
			}
			calls := &mockclient.CallCounter{}
			c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objects...).WithInterceptorFuncs(calls.Interceptors()).Build()
			r := &Controller{client: c, reader: c, label: meta.TenantLabel}
			req := reconcile.Request{Name: "pv-tenant-0"}
			if _, err := r.Reconcile(b.Context(), req); err != nil {
				b.Fatal(err)
			}
			pv := persistentVolumeTestVolume("tenant-0", nil)
			if err := c.Get(b.Context(), client.ObjectKeyFromObject(pv), pv); err != nil {
				b.Fatal(err)
			}
			if pv.Labels[meta.TenantLabel] != "tenant-0" {
				b.Fatal("tenant label was not reconciled")
			}
			calls.Reset()
			b.ReportAllocs()
			for b.Loop() {
				if _, err := r.Reconcile(b.Context(), req); err != nil {
					b.Fatal(err)
				}
			}
			calls.Report(b)
		})
	}
}
