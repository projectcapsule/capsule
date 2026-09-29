// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package resourcepools

import (
	"fmt"
	"testing"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/events"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/internal/metrics"
	mockclient "github.com/projectcapsule/capsule/internal/mocks/client"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/runtime/selectors"
)

func BenchmarkControllerResourcePool(b *testing.B) {
	for _, kind := range []string{"pool", "claim"} {
		for _, count := range []int{1, 32} {
			b.Run(fmt.Sprintf("%s/steady/namespaces=%d", kind, count), func(b *testing.B) {
				scheme := runtime.NewScheme()
				for _, add := range []func(*runtime.Scheme) error{corev1.AddToScheme, capsulev1beta2.AddToScheme} {
					if err := add(scheme); err != nil {
						b.Fatal(err)
					}
				}
				disabled := false
				pool := &capsulev1beta2.ResourcePool{
					Name: "pool",
					UID:  "pool-uid",
					Spec: capsulev1beta2.ResourcePoolSpec{
						Config:    capsulev1beta2.ResourcePoolSpecConfiguration{DefaultsAssignZero: &disabled, OrderedQueue: &disabled, DeleteBoundResources: &disabled},
						Selectors: []selectors.NamespaceSelector{{LabelSelector: &metav1.LabelSelector{MatchLabels: map[string]string{meta.TenantLabel: "tenant-a"}}}},
						Quota:     corev1.ResourceQuotaSpec{Hard: rl(map[corev1.ResourceName]string{corev1.ResourceCPU: "1000"})},
					},
				}
				objects := []client.Object{
					pool,
					&capsulev1beta2.Tenant{Name: "tenant-a"},
					&capsulev1beta2.Tenant{Name: "tenant-b"},
					&corev1.Namespace{Name: "unrelated", Labels: map[string]string{meta.TenantLabel: "tenant-b"}},
				}
				for i := range count {
					name := fmt.Sprintf("team-%d", i)
					objects = append(objects, &corev1.Namespace{
						Name:   name,
						Labels: map[string]string{meta.TenantLabel: "tenant-a"},
						Status: corev1.NamespaceStatus{Phase: corev1.NamespaceActive},
					}, &capsulev1beta2.ResourcePoolClaim{
						Name:      "claim",
						Namespace: name,
						UID:       types.UID(name),
						Spec:      capsulev1beta2.ResourcePoolClaimSpec{Pool: pool.Name, ResourceClaims: rl(map[corev1.ResourceName]string{corev1.ResourceCPU: "1"})},
						Status:    capsulev1beta2.ResourcePoolClaimStatus{Pool: meta.LocalRFC1123ObjectReferenceWithUID{Name: "pool", UID: pool.UID}},
					})
				}
				calls := &mockclient.CallCounter{}
				c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objects...).WithStatusSubresource(pool, &capsulev1beta2.ResourcePoolClaim{}, &corev1.ResourceQuota{}).WithIndex(&capsulev1beta2.ResourcePoolClaim{}, ".status.pool.uid", func(obj client.Object) []string {
					return []string{string(obj.(*capsulev1beta2.ResourcePoolClaim).Status.Pool.UID)}
				}).WithInterceptorFuncs(calls.Interceptors()).Build()
				pools := &resourcePoolController{
					Client:   c,
					reader:   c,
					log:      logr.Discard(),
					metrics:  metrics.NewResourcePoolRecorder(),
					recorder: &events.FakeRecorder{},
				}
				req := reconcile.Request{Name: pool.Name}
				if _, err := pools.Reconcile(b.Context(), req); err != nil {
					b.Fatal(err)
				}
				if err := c.Get(b.Context(), client.ObjectKeyFromObject(pool), pool); err != nil {
					b.Fatal(err)
				}
				claimed := pool.Status.Allocation.Claimed[corev1.ResourceCPU]
				if len(pool.Status.Namespaces) != count || pool.Status.ClaimSize != uint(count) ||
					claimed.Value() != int64(count) || !meta.IsStatusConditionTrue(pool.Status.Conditions, meta.ReadyCondition) {
					b.Fatalf("pool did not reconcile: %#v", pool.Status)
				}
				var r reconcile.Reconciler = pools
				if kind == "claim" {
					r = &resourceClaimController{
						Client:   c,
						reader:   c,
						log:      logr.Discard(),
						metrics:  metrics.NewClaimRecorder(),
						recorder: &events.FakeRecorder{},
					}
					req = reconcile.Request{Namespace: "team-0", Name: "claim"}
					if _, err := r.Reconcile(b.Context(), req); err != nil {
						b.Fatal(err)
					}
					claim := &capsulev1beta2.ResourcePoolClaim{}
					if err := c.Get(b.Context(), req.NamespacedName, claim); err != nil {
						b.Fatal(err)
					}
					if claim.Status.Pool.UID != pool.UID || !meta.IsStatusConditionTrue(claim.Status.Conditions, meta.ReadyCondition) {
						b.Fatalf("claim was not assigned: %#v", claim.Status)
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
}
