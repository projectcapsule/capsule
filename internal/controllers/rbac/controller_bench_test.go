// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package rbac

import (
	"fmt"
	"testing"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	mockclient "github.com/projectcapsule/capsule/internal/mocks/client"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/runtime/configuration"
)

func BenchmarkControllerRBAC(b *testing.B) {
	for _, count := range []int{1, 100} {
		for _, kind := range []string{"roles", "bindings"} {
			b.Run(fmt.Sprintf("%s/promoted=%d", kind, count), func(b *testing.B) {
				scheme := runtime.NewScheme()
				for _, add := range []func(*runtime.Scheme) error{corev1.AddToScheme, rbacv1.AddToScheme, capsulev1beta2.AddToScheme} {
					if err := add(scheme); err != nil {
						b.Fatal(err)
					}
				}
				configObject := &capsulev1beta2.CapsuleConfiguration{
					Name: "capsule",
					Spec: capsulev1beta2.CapsuleConfigurationSpec{
						AllowServiceAccountPromotion: true,
						RBAC:                         &capsulev1beta2.RBACConfiguration{ProvisionerClusterRole: "provisioner", DeleterClusterRole: "deleter"},
					},
				}
				objects := []client.Object{configObject, &capsulev1beta2.Tenant{Name: "tenant-a"}, &capsulev1beta2.Tenant{Name: "tenant-b"}}
				for i := range count {
					objects = append(objects, &corev1.ServiceAccount{
						Name:      fmt.Sprintf("owner-%d", i),
						Namespace: fmt.Sprintf("tenant-%s", []string{"a", "b"}[i%2]),
						Labels:    map[string]string{meta.OwnerPromotionLabel: meta.ValueTrue},
					})
				}
				calls := &mockclient.CallCounter{}
				c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objects...).WithInterceptorFuncs(calls.Interceptors()).Build()
				r := &Manager{
					Client:        c,
					reader:        c,
					Log:           logr.Discard(),
					Configuration: configuration.NewCapsuleConfiguration(b.Context(), c, c, nil, "capsule"),
				}
				req := reconcile.Request{Name: "provisioner"}
				if kind == "bindings" {
					req.Namespace = serviceAccountEventMarker
				}
				if _, err := r.Reconcile(b.Context(), req); err != nil {
					b.Fatal(err)
				}
				if kind == "bindings" {
					obj := &rbacv1.ClusterRoleBinding{}
					if err := c.Get(b.Context(), client.ObjectKey{Name: "provisioner"}, obj); err != nil {
						b.Fatal(err)
					}
					if len(obj.Subjects) != count {
						b.Fatalf("got %d subjects, want %d", len(obj.Subjects), count)
					}
				} else {
					obj := &rbacv1.ClusterRole{}
					if err := c.Get(b.Context(), client.ObjectKey{Name: "provisioner"}, obj); err != nil {
						b.Fatal(err)
					}
					if len(obj.Rules) == 0 {
						b.Fatal("empty role")
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
