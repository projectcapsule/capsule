// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package generic

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	clientgocache "k8s.io/client-go/tools/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/runtime/indexers/tenantresource"
)

func BenchmarkReplicationAdmissionFreshness(b *testing.B) {
	for _, tenants := range []int{1, 100, 1000} {
		for _, scenario := range []string{"unprotected", "deny", "allow", "missing-parent"} {
			b.Run(fmt.Sprintf("tenants=%d/%s", tenants, scenario), func(b *testing.B) {
				protected := scenario == "deny" || scenario == "allow"
				c, req := replicationAdmissionFixture(b, false, protected, tenants)
				if scenario == "allow" {
					req.UserInfo.Username = "system:serviceaccount:tenant-a:runner"
				}
				if scenario == "missing-parent" {
					req.OldObject.Raw = []byte(`{"apiVersion":"v1","kind":"ConfigMap","metadata":{"name":"item","namespace":"tenant-a","labels":{"protection.projectcapsule.dev/replications":"true"}}}`)
				}
				// Use the manager's client-go index implementation; fake List scans
				// unrelated objects and does not model the production lookup cost.
				index := clientgocache.NewIndexer(clientgocache.MetaNamespaceKeyFunc, clientgocache.Indexers{
					tenantresource.ProtectedIndexerFieldName: func(obj any) ([]string, error) {
						return (tenantresource.ProtectedItems{}).Func()(obj.(client.Object)), nil
					},
				})
				parents := &capsulev1beta2.TenantResourceList{}
				require.NoError(b, c.List(b.Context(), parents))
				for i := range parents.Items {
					require.NoError(b, index.Add(&parents.Items[i]))
				}
				lists, reads := 0, 0
				c = interceptor.NewClient(c.(client.WithWatch), interceptor.Funcs{
					List: func(_ context.Context, _ client.WithWatch, list client.ObjectList, opts ...client.ListOption) error {
						lists++
						selector := (&client.ListOptions{}).ApplyOptions(opts).FieldSelector
						if selector == nil {
							b.Fatal("unindexed lookup")
						}
						key, ok := selector.RequiresExactMatch(tenantresource.ProtectedIndexerFieldName)
						if !ok {
							b.Fatal("incorrect lookup field")
						}
						if local, ok := list.(*capsulev1beta2.TenantResourceList); ok {
							matches, err := index.ByIndex(tenantresource.ProtectedIndexerFieldName, key)
							if err != nil {
								return err
							}
							for _, obj := range matches {
								local.Items = append(local.Items, *obj.(*capsulev1beta2.TenantResource).DeepCopy())
							}
						}
						return nil
					},
					Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
						reads++
						return c.Get(ctx, key, obj, opts...)
					},
				})
				handler := ReplicaHandler().OnUpdate(c, c, admission.NewDecoder(c.Scheme()), nil)
				b.ReportAllocs()
				for b.Loop() {
					response := handler(b.Context(), req)
					wantDenied := scenario == "deny" || scenario == "missing-parent"
					if (response != nil) != wantDenied || (response != nil && response.Result.Code != 403) {
						b.Fatal("unexpected admission decision")
					}
				}
				b.ReportMetric(float64(lists)/float64(b.N), "indexed-lists/op")
				b.ReportMetric(float64(reads)/float64(b.N), "parent-reads/op")
			})
		}
	}
}
