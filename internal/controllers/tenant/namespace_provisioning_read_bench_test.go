// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package tenant

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	mockclient "github.com/projectcapsule/capsule/internal/mocks/client"
	"github.com/projectcapsule/capsule/pkg/api/meta"
)

func BenchmarkNamespaceProvisioningTransientRead(b *testing.B) {
	for _, tenants := range []int{1, 16} {
		for _, count := range []int{1, 32} {
			for _, parallel := range []bool{false, true} {
				b.Run(fmt.Sprintf("tenants=%d/namespaces=%d/parallel=%v", tenants, count, parallel), func(b *testing.B) {
					tnt := provisioningTenant()
					objects := []client.Object{tnt}
					namespaces := make([]*corev1.Namespace, count)
					for i := range count {
						ns := cleanupOwnedNamespace(tnt, fmt.Sprintf("owned-%d", i), false)
						namespaces[i] = ns
						objects = append(objects, ns)
						metadata := &capsulev1beta2.TenantStatusNamespaceMetadata{Labels: map[string]string{}, Annotations: map[string]string{}}
						for j := range 32 {
							key := fmt.Sprintf("example.com/managed-%d", j)
							metadata.Labels[key], metadata.Annotations[key] = "value", "value"
						}
						tnt.Status.Spaces = append(tnt.Status.Spaces, &capsulev1beta2.TenantStatusNamespaceItem{Name: ns.Name, UID: ns.UID, Metadata: metadata, Conditions: meta.ConditionList{meta.NewReadyCondition(ns)}})
					}
					for i := 1; i < tenants; i++ {
						other := &capsulev1beta2.Tenant{Name: fmt.Sprintf("tenant-%d", i), UID: types.UID(fmt.Sprintf("tenant-%d", i))}
						objects = append(objects, other, cleanupOwnedNamespace(other, other.Name+"-ns", false))
					}
					r, _ := namespaceCleanupFixture(b, objects...)
					failure := errors.New("namespace unavailable")
					failing := interceptor.NewClient(r.Client.(client.WithWatch), interceptor.Funcs{Get: func(context.Context, client.WithWatch, client.ObjectKey, client.Object, ...client.GetOption) error {
						return failure
					}})
					calls := &mockclient.CallCounter{}
					r.provisioningReader = interceptor.NewClient(failing, calls.Interceptors())
					plan := prepareNamespaceRoleBindings(tnt)
					run := func() {
						_, err := r.provisionNamespace(b.Context(), logr.Discard(), namespaces[count-1], tnt, &plan)
						if !errors.Is(err, failure) {
							b.Errorf("unexpected error: %v", err)
						}
					}
					b.ReportAllocs()
					b.ResetTimer()
					if parallel {
						b.RunParallel(func(pb *testing.PB) {
							for pb.Next() {
								run()
							}
						})
					} else {
						for range b.N {
							run()
						}
					}
					b.StopTimer()
					calls.Report(b)
				})
			}
		}
	}
}
