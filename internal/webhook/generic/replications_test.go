// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package generic

import (
	"context"
	"fmt"
	"testing"

	authenticationv1 "k8s.io/api/authentication/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	apiruntime "github.com/projectcapsule/capsule/pkg/api/runtime"
	"github.com/projectcapsule/capsule/pkg/runtime/indexers/tenantresource"
)

func TestReplicationPolicyAdmission(t *testing.T) {
	for _, global := range []bool{false, true} {
		for _, protected := range []bool{false, true} {
			for _, authorized := range []bool{false, true} {
				t.Run(fmt.Sprintf("global=%t/protected=%t/authorized=%t", global, protected, authorized), func(t *testing.T) {
					c, req := replicationAdmissionFixture(t, global, protected, 1)
					if authorized {
						req.UserInfo.Username = "system:serviceaccount:tenant-a:runner"
					}
					for _, handler := range []func(context.Context, admission.Request) *admission.Response{ReplicaHandler().OnUpdate(c, nil, nil, nil), ReplicaHandler().OnDelete(c, nil, nil, nil)} {
						response := handler(t.Context(), req)
						wantDenied := protected && !authorized
						if wantDenied && (response == nil || response.Allowed || response.Result.Code != 403) {
							t.Fatalf("expected policy denial, got %#v", response)
						}
						if !wantDenied && response != nil {
							t.Fatalf("expected allowed chain continuation, got %#v", response)
						}
						req.Namespace = "tenant-b"
						if response := handler(t.Context(), req); response != nil {
							t.Fatalf("policy leaked to other namespace: %#v", response)
						}
						req.Namespace = "tenant-a"
					}
				})
			}
		}
	}
}

func BenchmarkReplicationPolicyAdmission(b *testing.B) {
	for _, n := range []int{1, 100} {
		for _, protected := range []bool{false, true} {
			b.Run(fmt.Sprintf("tenants=%d/protected=%t", n, protected), func(b *testing.B) {
				c, req := replicationAdmissionFixture(b, false, protected, n)
				handler := ReplicaHandler().OnUpdate(c, nil, nil, nil)
				b.ReportAllocs()
				for b.Loop() {
					response := handler(b.Context(), req)
					if (response != nil) != protected {
						b.Fatal("unexpected admission decision")
					}
				}
			})
		}
	}
}

// The fake client verifies indexed query semantics, not production cache latency.
func replicationAdmissionFixture(t testing.TB, global, protected bool, tenants int) (client.Client, admission.Request) {
	t.Helper()
	scheme := runtime.NewScheme()
	if err := capsulev1beta2.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	builder := fake.NewClientBuilder().WithScheme(scheme)
	for _, obj := range []client.Object{&capsulev1beta2.GlobalTenantResource{}, &capsulev1beta2.TenantResource{}} {
		index := tenantresource.ProtectedItems{Obj: obj}
		builder.WithIndex(obj, index.Field(), index.Func())
	}
	for i := range tenants {
		namespace := "tenant-a"
		if i > 0 {
			namespace = fmt.Sprintf("other-tenant-%d", i)
		}
		status := capsulev1beta2.TenantResourceCommonStatus{
			ServiceAccount: &meta.NamespacedRFC1123ObjectReferenceWithNamespace{Name: "runner", Namespace: meta.RFC1123SubdomainName(namespace)},
			ProcessedItems: meta.ProcessedItems{{Version: "v1", Kind: "ConfigMap", Namespace: namespace, Name: "item", Policy: &apiruntime.ResourceTemplatePolicy{Creation: apiruntime.ResourceCreationPolicyMerge, Protect: new(protected)}}},
		}
		if global {
			builder.WithObjects(&capsulev1beta2.GlobalTenantResource{Name: namespace, Status: capsulev1beta2.GlobalTenantResourceStatus{TenantResourceCommonStatus: status}})
		} else {
			builder.WithObjects(&capsulev1beta2.TenantResource{Name: "parent", Namespace: namespace, Status: capsulev1beta2.TenantResourceStatus{TenantResourceCommonStatus: status}})
		}
	}
	return builder.Build(), admission.Request{Kind: metav1.GroupVersionKind{Version: "v1", Kind: "ConfigMap"}, Name: "item", Namespace: "tenant-a", UserInfo: authenticationv1.UserInfo{Username: "tenant-owner"}}
}
