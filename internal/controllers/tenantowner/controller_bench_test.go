// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package tenantowners

import (
	"fmt"
	"testing"

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/internal/metrics"
	mockclient "github.com/projectcapsule/capsule/internal/mocks/client"
	"github.com/projectcapsule/capsule/pkg/api/rbac"
	indexer "github.com/projectcapsule/capsule/pkg/runtime/indexers/tenant"
)

func BenchmarkControllerTenantOwner(b *testing.B) {
	for _, count := range []int{1, 100} {
		b.Run(fmt.Sprintf("steady/matched=%d/unrelated=%d", count, count), func(b *testing.B) {
			scheme := runtime.NewScheme()
			if err := capsulev1beta2.AddToScheme(scheme); err != nil {
				b.Fatal(err)
			}
			owner := &capsulev1beta2.TenantOwner{
				Name: "owner",
				Spec: capsulev1beta2.TenantOwnerSpec{CoreOwnerSpec: rbac.CoreOwnerSpec{Name: "alice", Kind: rbac.UserOwner}},
			}
			objects := []client.Object{owner}
			for i := range count {
				for _, name := range []string{"alice", "bob"} {
					tnt := &capsulev1beta2.Tenant{Name: fmt.Sprintf("%s-%d", name, i)}
					tnt.Status.Owners = rbac.OwnerStatusListSpec{{Name: name, Kind: rbac.UserOwner}}
					objects = append(objects, tnt)
				}
			}
			calls := &mockclient.CallCounter{}
			c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objects...).WithStatusSubresource(owner).WithIndex(&capsulev1beta2.Tenant{}, indexer.OwnerKindIndexerFieldName, indexer.OwnerReference{}.Func()).WithInterceptorFuncs(calls.Interceptors()).Build()
			r := &TenantOwnerManager{Client: c, reader: c, metrics: metrics.NewTenantOwnerRecorder(), Log: logr.Discard()}
			req := ctrl.Request{Name: owner.Name}
			if _, err := r.Reconcile(b.Context(), req); err != nil {
				b.Fatal(err)
			}
			if err := c.Get(b.Context(), client.ObjectKeyFromObject(owner), owner); err != nil {
				b.Fatal(err)
			}
			if len(owner.Status.Tenants) != count {
				b.Fatalf("matched %d tenants, want %d", len(owner.Status.Tenants), count)
			}
			for _, name := range owner.Status.Tenants {
				if name[:5] != "alice" {
					b.Fatal("unrelated tenant matched")
				}
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
