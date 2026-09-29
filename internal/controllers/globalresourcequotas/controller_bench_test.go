// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package globalresourcequotas

import (
	"fmt"
	"testing"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/internal/metrics"
	mockclient "github.com/projectcapsule/capsule/internal/mocks/client"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/runtime/selectors"
)

func BenchmarkControllerGlobalResourceQuota(b *testing.B) {
	for _, count := range []int{1, 32} {
		b.Run(fmt.Sprintf("steady/namespaces=%d", count), func(b *testing.B) {
			scheme := runtime.NewScheme()
			for _, add := range []func(*runtime.Scheme) error{corev1.AddToScheme, capsulev1beta2.AddToScheme} {
				if err := add(scheme); err != nil {
					b.Fatal(err)
				}
			}
			quota := &capsulev1beta2.GlobalResourceQuota{
				Name: "quota",
				UID:  "quota-uid",
				Spec: capsulev1beta2.GlobalResourceQuotaSpec{
					NamespaceSelectors: []selectors.NamespaceSelector{{LabelSelector: &metav1.LabelSelector{MatchLabels: map[string]string{meta.TenantLabel: "tenant-a"}}}},
					Quota:              corev1.ResourceQuotaSpec{Hard: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("1000")}},
				},
			}
			objects := []client.Object{
				quota,
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
				}, observedResourceQuota(quota, name, corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("1")}))
			}
			calls := &mockclient.CallCounter{}
			c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objects...).WithStatusSubresource(quota, &capsulev1beta2.QuantityLedger{}, &corev1.ResourceQuota{}).WithInterceptorFuncs(calls.Interceptors()).Build()
			r := &Controller{Client: c, reader: c, log: logr.Discard(), metrics: metrics.NewGlobalResourceQuotaRecorder()}
			req := reconcile.Request{Name: quota.Name}
			for range 2 {
				if _, err := r.Reconcile(b.Context(), req); err != nil {
					b.Fatal(err)
				}
			}
			if err := c.Get(b.Context(), client.ObjectKeyFromObject(quota), quota); err != nil {
				b.Fatal(err)
			}
			used := quota.Status.Total.Used[corev1.ResourceCPU]
			if len(quota.Status.Namespaces) != count || used.Value() != int64(count) || !meta.IsStatusConditionTrue(quota.Status.Conditions, meta.ReadyCondition) {
				b.Fatalf("incomplete quota status: %#v", quota.Status)
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
