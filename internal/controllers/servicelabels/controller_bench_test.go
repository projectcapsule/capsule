// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package servicelabels

import (
	"fmt"
	"testing"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	mockclient "github.com/projectcapsule/capsule/internal/mocks/client"
	"github.com/projectcapsule/capsule/pkg/api"
	"github.com/projectcapsule/capsule/pkg/api/meta"
)

func BenchmarkControllerServiceMetadata(b *testing.B) {
	for _, kind := range []string{"Service", "EndpointSlice"} {
		b.Run(kind, func(b *testing.B) {
			for _, tenants := range []int{1, 16} {
				for _, labels := range []int{1, 32} {
					b.Run(fmt.Sprintf("steady/tenants=%d/labels=%d", tenants, labels), func(b *testing.B) {
						ctx := b.Context()
						scheme := runtime.NewScheme()
						for _, add := range []func(*runtime.Scheme) error{corev1.AddToScheme, capsulev1beta2.AddToScheme, discoveryv1.AddToScheme} {
							if err := add(scheme); err != nil {
								b.Fatal(err)
							}
						}
						metadata := map[string]string{}
						for i := range labels {
							metadata[fmt.Sprintf("example.com/key-%d", i)] = "value"
						}
						objects := []client.Object{}
						for i := range tenants {
							name := fmt.Sprintf("tenant-%d", i)
							tnt := &capsulev1beta2.Tenant{Name: name, UID: types.UID(name)}
							tnt.Spec.ServiceOptions = &api.ServiceOptions{AdditionalMetadata: &api.AdditionalMetadataSpec{Labels: metadata}}
							ns := &corev1.Namespace{
								Name:            name,
								Labels:          map[string]string{meta.TenantLabel: name},
								OwnerReferences: []metav1.OwnerReference{{APIVersion: capsulev1beta2.GroupVersion.String(), Kind: "Tenant", Name: name, UID: tnt.UID}},
							}
							objects = append(objects, tnt, ns)
						}
						var obj client.Object = &corev1.Service{Name: "object", Namespace: "tenant-0"}
						if kind == "EndpointSlice" {
							obj = &discoveryv1.EndpointSlice{Name: "object", Namespace: "tenant-0", AddressType: discoveryv1.AddressTypeIPv4}
						}
						objects = append(objects, obj)
						calls := &mockclient.CallCounter{}
						c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objects...).WithInterceptorFuncs(calls.Interceptors()).Build()
						r := &abstractServiceLabelsReconciler{obj: obj.DeepCopyObject().(client.Object), client: c, log: logr.Discard()}
						request := ctrl.Request{NamespacedName: client.ObjectKeyFromObject(obj)}
						if _, err := r.Reconcile(ctx, request); err != nil {
							b.Fatal(err)
						}
						if err := c.Get(ctx, request.NamespacedName, obj); err != nil {
							b.Fatal(err)
						}
						if len(obj.GetLabels()) != labels {
							b.Fatalf("metadata was not reconciled: %v", obj.GetLabels())
						}
						calls.Reset()
						b.ReportAllocs()
						for b.Loop() {
							if _, err := r.Reconcile(ctx, request); err != nil {
								b.Fatal(err)
							}
						}
						calls.Report(b)
					})
				}
			}
		})
	}
}
