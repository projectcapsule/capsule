// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package config

import (
	"fmt"
	"testing"

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/internal/metrics"
	mockclient "github.com/projectcapsule/capsule/internal/mocks/client"
	"github.com/projectcapsule/capsule/pkg/api/rbac"
)

func BenchmarkControllerConfiguration(b *testing.B) {
	for _, count := range []int{1, 100} {
		b.Run(fmt.Sprintf("tenant-event/tenants=%d/owners=%d", count, count), func(b *testing.B) {
			scheme := runtime.NewScheme()
			if err := capsulev1beta2.AddToScheme(scheme); err != nil {
				b.Fatal(err)
			}
			cfg := &capsulev1beta2.CapsuleConfiguration{Name: "capsule"}
			objects := []client.Object{cfg}
			for i := range count {
				name := fmt.Sprintf("tenant-%d", i)
				objects = append(objects, &capsulev1beta2.Tenant{Name: name}, &capsulev1beta2.TenantOwner{Name: name, Spec: capsulev1beta2.TenantOwnerSpec{CoreOwnerSpec: rbac.CoreOwnerSpec{Name: name, Kind: rbac.UserOwner}}})
			}
			calls := &mockclient.CallCounter{}
			c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objects...).WithStatusSubresource(cfg).WithInterceptorFuncs(calls.Interceptors()).Build()
			r := &Manager{Client: c, reader: c, metrics: metrics.NewConfigRecorder(), Log: logr.Discard()}
			req := reconcile.Request{Name: cfg.Name, Namespace: tenantEventMarker}
			if _, err := r.Reconcile(b.Context(), req); err != nil {
				b.Fatal(err)
			}
			if err := c.Get(b.Context(), client.ObjectKeyFromObject(cfg), cfg); err != nil {
				b.Fatal(err)
			}
			if len(cfg.Status.Tenants) != count || len(cfg.Status.Users) != count {
				b.Fatalf("incomplete status: %#v", cfg.Status)
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
