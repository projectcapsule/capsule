// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package customquotas

import (
	"fmt"
	"testing"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	k8smeta "k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/internal/cache"
	"github.com/projectcapsule/capsule/internal/metrics"
	mockclient "github.com/projectcapsule/capsule/internal/mocks/client"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	apiruntime "github.com/projectcapsule/capsule/pkg/api/runtime"
	"github.com/projectcapsule/capsule/pkg/runtime/quota"
	"github.com/projectcapsule/capsule/pkg/runtime/selectors"
)

func BenchmarkControllerCustomQuota(b *testing.B) {
	for _, global := range []bool{false, true} {
		for _, count := range []int{1, 100} {
			b.Run(fmt.Sprintf("global=%t/steady/objects=%d", global, count), func(b *testing.B) {
				scheme := runtime.NewScheme()
				for _, add := range []func(*runtime.Scheme) error{corev1.AddToScheme, capsulev1beta2.AddToScheme} {
					if err := add(scheme); err != nil {
						b.Fatal(err)
					}
				}
				mapper := k8smeta.NewDefaultRESTMapper([]schema.GroupVersion{corev1.SchemeGroupVersion})
				mapper.Add(corev1.SchemeGroupVersion.WithKind("ConfigMap"), k8smeta.RESTScopeNamespace)
				spec := capsulev1beta2.CustomQuotaSpec{
					Options: &capsulev1beta2.CustomQuotaOptionsSpec{},
					Limit:   resource.MustParse("1000"),
					Sources: []capsulev1beta2.CustomQuotaSpecSource{
						{
							VersionKind:                 apiruntime.VersionKind{APIVersion: "v1", Kind: "ConfigMap"},
							CustomQuotaSpecSourceConfig: capsulev1beta2.CustomQuotaSpecSourceConfig{Operation: quota.Operation("count")},
						},
					},
				}
				local := &capsulev1beta2.CustomQuota{Name: "quota", Namespace: "tenant-a", UID: "quota-uid", Spec: spec}
				cluster := &capsulev1beta2.GlobalCustomQuota{
					Name: "quota",
					UID:  "quota-uid",
					Spec: capsulev1beta2.GlobalCustomQuotaSpec{
						CustomQuotaSpec:    spec,
						NamespaceSelectors: []selectors.NamespaceSelector{{LabelSelector: &metav1.LabelSelector{MatchLabels: map[string]string{meta.TenantLabel: "tenant-a"}}}},
					},
				}
				var obj client.Object = local
				if global {
					obj = cluster
				}
				objects := []client.Object{obj}
				for _, tenant := range []string{"tenant-a", "tenant-b"} {
					objects = append(objects, &capsulev1beta2.Tenant{Name: tenant}, &corev1.Namespace{
						Name:   tenant,
						Labels: map[string]string{meta.TenantLabel: tenant},
						Status: corev1.NamespaceStatus{Phase: corev1.NamespaceActive},
					})
					for i := range count {
						objects = append(objects, &corev1.ConfigMap{Name: fmt.Sprintf("object-%d", i), Namespace: tenant})
					}
				}
				calls := &mockclient.CallCounter{}
				c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objects...).WithStatusSubresource(local, cluster, &capsulev1beta2.QuantityLedger{}).WithInterceptorFuncs(calls.Interceptors()).Build()
				cel, err := cache.NewCELCache()
				if err != nil {
					b.Fatal(err)
				}
				var r reconcile.Reconciler = &customQuotaClaimController{
					Client:        c,
					reader:        c,
					log:           logr.Discard(),
					metrics:       metrics.NewCustomQuotaRecorder(),
					mapper:        mapper,
					jsonPathCache: cache.NewJSONPathCache(),
					celCache:      cel,
					targetsCache:  cache.NewCompiledTargetsCache[string](),
				}
				if global {
					r = &clusterCustomQuotaClaimController{
						Client:        c,
						reader:        c,
						log:           logr.Discard(),
						metrics:       metrics.NewGlobalCustomQuotaRecorder(),
						mapper:        mapper,
						jsonPathCache: cache.NewJSONPathCache(),
						celCache:      cel,
						targetsCache:  cache.NewCompiledTargetsCache[string](),
					}
				}
				req := reconcile.Request{NamespacedName: client.ObjectKeyFromObject(obj)}
				if _, err := r.Reconcile(b.Context(), req); err != nil {
					b.Fatal(err)
				}
				if err := c.Get(b.Context(), req.NamespacedName, obj); err != nil {
					b.Fatal(err)
				}
				status := local.Status
				if global {
					status = cluster.Status.CustomQuotaStatus
				}
				if status.Usage.Used.Value() != int64(count) || !meta.IsStatusConditionTrue(status.Conditions, meta.ReadyCondition) {
					b.Fatalf("wrong quota usage or readiness: %#v", status)
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
