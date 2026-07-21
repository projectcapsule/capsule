// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package resources

import (
	"context"
	"fmt"
	"testing"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	capsulemeta "github.com/projectcapsule/capsule/pkg/api/meta"
	capruntime "github.com/projectcapsule/capsule/pkg/api/runtime"
	"github.com/projectcapsule/capsule/pkg/runtime/indexers/tenantresource"
	"github.com/projectcapsule/capsule/pkg/runtime/watch"
)

// BenchmarkNamespacedTriggerSink_Notify measures the per-event cost of the
// hottest sink path: one watched-object change fanned against N indexed
// TenantResources across 10 tenants. The fake client is slower than the
// production informer-backed reader (it deep-copies from its tracker), so
// this is an upper bound on real dispatch cost.
func BenchmarkNamespacedTriggerSink_Notify(b *testing.B) {
	for _, n := range []int{100, 1000} {
		b.Run(fmt.Sprintf("tenantresources=%d", n), func(b *testing.B) {
			scheme := testScheme(b)

			objs := make([]client.Object, 0, n+10)

			for i := 0; i < 10; i++ {
				objs = append(objs, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{
					Name:   fmt.Sprintf("tenant-%d", i),
					Labels: map[string]string{capsulemeta.TenantLabel: fmt.Sprintf("tenant-%d", i)},
				}})
			}

			for i := 0; i < n; i++ {
				tr := &capsulev1beta2.TenantResource{ObjectMeta: metav1.ObjectMeta{
					Name:      fmt.Sprintf("tr-%d", i),
					Namespace: fmt.Sprintf("tenant-%d", i%10),
				}}
				tr.Spec.Triggers = []capsulev1beta2.TriggerSpec{{
					VersionKinds: capruntime.VersionKinds{Kinds: []string{"ConfigMap"}},
				}}
				objs = append(objs, tr)
			}

			cl := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(objs...).
				WithIndex(&capsulev1beta2.TenantResource{}, tenantresource.TriggersIndexerFieldName, tenantresource.NamespacedTriggers{}.Func()).
				Build()

			sink := &namespacedTriggerSink{
				reader:  cl,
				enqueue: func(types.NamespacedName) {},
				log:     logr.Discard(),
			}

			obj := &metav1.PartialObjectMetadata{ObjectMeta: metav1.ObjectMeta{Name: "cm", Namespace: "tenant-3"}}
			ctx := context.Background()

			b.ReportAllocs()
			b.ResetTimer()

			for i := 0; i < b.N; i++ {
				sink.Notify(ctx, configMapGVK, watch.OperationUpdate, obj)
			}
		})
	}
}
