// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package resources

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	k8smeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/internal/metrics"
	mockclient "github.com/projectcapsule/capsule/internal/mocks/client"
	"github.com/projectcapsule/capsule/pkg/api"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/api/processor"
	"github.com/projectcapsule/capsule/pkg/runtime/configuration"
)

func BenchmarkControllerReplication(b *testing.B) {
	for _, kind := range []string{"global", "namespaced", "namespace-watcher"} {
		for _, count := range []int{1, 32} {
			b.Run(fmt.Sprintf("%s/steady/namespaces=%d", kind, count), func(b *testing.B) {
				scheme := runtime.NewScheme()
				for _, add := range []func(*runtime.Scheme) error{corev1.AddToScheme, capsulev1beta2.AddToScheme} {
					if err := add(scheme); err != nil {
						b.Fatal(err)
					}
				}
				disabled, enabled := false, true
				common := func(name string) capsulev1beta2.TenantResourceCommonSpec {
					return capsulev1beta2.TenantResourceCommonSpec{
						Cordoned:        &disabled,
						Settings:        capsulev1beta2.TenantResourceCommonSpecSettings{Adopt: &disabled, Force: &disabled},
						PruningOnDelete: &enabled,
						ResyncPeriod:    metav1.Duration{Duration: time.Minute},
						Resources: []capsulev1beta2.ResourceSpec{
							{
								RawItems: []capsulev1beta2.RawExtension{
									{
										Raw: []byte(fmt.Sprintf("{\"apiVersion\":\"v1\",\"kind\":\"ConfigMap\",\"metadata\":{\"name\":%q},\"data\":{\"tenant\":\"{{tenant.name}}\"}}", name)),
									},
								},
							},
						},
					}
				}
				global := &capsulev1beta2.GlobalTenantResource{
					APIVersion: capsulev1beta2.GroupVersion.String(),
					Kind:       "GlobalTenantResource",
					Name:       "global",
					UID:        "global-uid",
					Spec: capsulev1beta2.GlobalTenantResourceSpec{
						TenantResourceCommonSpec: common("global"),
						Scope:                    api.ResourceScopeNamespace,
						TenantSelector:           metav1.LabelSelector{MatchLabels: map[string]string{"selected": "true"}},
					},
				}
				local := &capsulev1beta2.TenantResource{
					APIVersion: capsulev1beta2.GroupVersion.String(),
					Kind:       "TenantResource",
					Name:       "local",
					Namespace:  "tenant-a-0",
					UID:        "local-uid",
					Spec:       capsulev1beta2.TenantResourceSpec{TenantResourceCommonSpec: common("local")},
				}
				cfg := &capsulev1beta2.CapsuleConfiguration{Name: "capsule"}
				objects := []client.Object{cfg, global, local}
				for _, name := range []string{"tenant-a", "tenant-b"} {
					tnt := &capsulev1beta2.Tenant{Name: name, UID: types.UID(name)}
					if name == "tenant-a" {
						tnt.Labels = map[string]string{"selected": "true"}
					}
					for i := range count {
						ns := fmt.Sprintf("%s-%d", name, i)
						tnt.Status.Namespaces = append(tnt.Status.Namespaces, ns)
						objects = append(objects, &corev1.Namespace{
							Name:            ns,
							Labels:          map[string]string{meta.TenantLabel: name},
							OwnerReferences: []metav1.OwnerReference{{APIVersion: capsulev1beta2.GroupVersion.String(), Kind: "Tenant", Name: name, UID: tnt.UID}},
						})
					}
					objects = append(objects, tnt)
				}
				mapper := k8smeta.NewDefaultRESTMapper([]schema.GroupVersion{corev1.SchemeGroupVersion})
				mapper.Add(corev1.SchemeGroupVersion.WithKind("ConfigMap"), k8smeta.RESTScopeNamespace)
				calls := &mockclient.CallCounter{}
				c := fake.NewClientBuilder().WithScheme(scheme).WithRESTMapper(mapper).WithObjects(objects...).WithStatusSubresource(global, local).WithReturnManagedFields().WithInterceptorFuncs(calls.Interceptors()).Build()
				configuration := configuration.NewCapsuleConfiguration(b.Context(), c, c, nil, cfg.Name)
				proc := processor.Processor{Configuration: configuration, GatherClient: c, Mapper: mapper, AllowCrossNamespaceSelection: true}
				collector := NewCollector(c, mapper)
				globalClients := impersonatedClientLoader[*capsulev1beta2.GlobalTenantResource]{client: c, configuration: configuration, resolve: globalServiceAccount}
				localClients := impersonatedClientLoader[*capsulev1beta2.TenantResource]{client: c, configuration: configuration, resolve: namespacedServiceAccount}
				var r reconcile.Reconciler
				req := reconcile.Request{Name: global.Name}
				switch kind {
				case "global":
					r = &globalResourceController{
						client:        c,
						reader:        c,
						log:           logr.Discard(),
						configuration: configuration,
						processor:     proc,
						collector:     collector,
						clients:       globalClients,
						metrics:       metrics.NewGlobalTenantResourceRecorder(),
					}
				case "namespaced":
					r = &namespacedResourceController{
						client:        c,
						reader:        c,
						log:           logr.Discard(),
						configuration: configuration,
						processor:     proc,
						collector:     collector,
						clients:       localClients,
						metrics:       metrics.NewTenantResourceRecorder(),
					}
					req = reconcile.Request{NamespacedName: client.ObjectKeyFromObject(local)}
				case "namespace-watcher":
					r = &NamespaceTrigger{client: c, reader: c, log: logr.Discard(), configuration: configuration, processor: proc, collector: collector, globalClients: globalClients, namespacedClients: localClients,
						globalStatus: scopedStatusPatcher[*capsulev1beta2.GlobalTenantResource]{client: c, reader: c, factory: func() *capsulev1beta2.GlobalTenantResource { return &capsulev1beta2.GlobalTenantResource{} }, status: func(obj *capsulev1beta2.GlobalTenantResource) *capsulev1beta2.TenantResourceCommonStatus {
							return &obj.Status.TenantResourceCommonStatus
						}},
						namespacedStatus: scopedStatusPatcher[*capsulev1beta2.TenantResource]{client: c, reader: c, factory: func() *capsulev1beta2.TenantResource { return &capsulev1beta2.TenantResource{} }, status: func(obj *capsulev1beta2.TenantResource) *capsulev1beta2.TenantResourceCommonStatus {
							return &obj.Status.TenantResourceCommonStatus
						}}}
					req = reconcile.Request{Name: "tenant-a-0"}
				}
				if _, err := r.Reconcile(b.Context(), req); err != nil {
					b.Fatal(err)
				}
				verify := func() {
					b.Helper()
					switch kind {
					case "global":
						if err := c.Get(b.Context(), client.ObjectKeyFromObject(global), global); err != nil {
							b.Fatal(err)
						}
						if !meta.IsStatusConditionTrue(global.Status.Conditions, meta.ReadyCondition) {
							b.Fatalf("global replication failed: %#v", global.Status)
						}
					case "namespaced":
						if err := c.Get(b.Context(), client.ObjectKeyFromObject(local), local); err != nil {
							b.Fatal(err)
						}
						if !meta.IsStatusConditionTrue(local.Status.Conditions, meta.ReadyCondition) {
							b.Fatalf("namespaced replication failed: %#v", local.Status)
						}
					}
					maps := &corev1.ConfigMapList{}
					if err := c.List(b.Context(), maps); err != nil {
						b.Fatal(err)
					}
					want := count
					if kind == "namespace-watcher" {
						want = 2
					}
					if len(maps.Items) != want {
						if err := c.Get(b.Context(), client.ObjectKeyFromObject(global), global); err != nil {
							b.Fatal(err)
						}
						if err := c.Get(b.Context(), client.ObjectKeyFromObject(local), local); err != nil {
							b.Fatal(err)
						}
						b.Fatalf("got %d replicated objects, want %d; global status=%+v local status=%+v", len(maps.Items), want, global.Status, local.Status)
					}
					for _, obj := range maps.Items {
						if obj.Data["tenant"] != "tenant-a" || !strings.HasPrefix(obj.Namespace, "tenant-a-") {
							b.Fatalf("incorrect tenant context: %s/%s: %v", obj.Namespace, obj.Name, obj.Data)
						}
					}
				}
				verify()
				calls.Reset()
				b.ReportAllocs()
				for b.Loop() {
					if _, err := r.Reconcile(b.Context(), req); err != nil {
						b.Fatal(err)
					}
				}
				calls.Report(b)
				verify()
			})
		}
	}
}
