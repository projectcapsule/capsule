// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package rulestatus

import (
	"fmt"
	"testing"

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/internal/metrics"
	mockclient "github.com/projectcapsule/capsule/internal/mocks/client"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/api/rules"
)

func BenchmarkControllerRuleStatus(b *testing.B) {
	for _, count := range []int{1, 100} {
		for _, mode := range []string{"steady", "publish"} {
			b.Run(fmt.Sprintf("%s/rules=%d", mode, count), func(b *testing.B) {
				scheme := runtime.NewScheme()
				if err := capsulev1beta2.AddToScheme(scheme); err != nil {
					b.Fatal(err)
				}
				obj := &capsulev1beta2.RuleStatus{Name: "rules", Namespace: "tenant-a", Generation: 1}
				for range count {
					obj.Spec = append(obj.Spec, &rules.NamespaceRuleBodyNamespace{Enforce: &rules.NamespaceRuleEnforceBody{Action: rules.ActionTypeDeny}})
				}
				calls := &mockclient.CallCounter{}
				// Keep an uninstrumented client for per-iteration generation changes.
				base := fake.NewClientBuilder().WithScheme(scheme).WithObjects(obj).WithStatusSubresource(obj).Build()
				c := interceptor.NewClient(base, calls.Interceptors())
				r := &Manager{Client: c, reader: c, Metrics: metrics.NewRuleStatusRecorder(), Log: logr.Discard()}
				req := ctrl.Request{NamespacedName: client.ObjectKeyFromObject(obj)}
				if _, err := r.Reconcile(b.Context(), req); err != nil {
					b.Fatal(err)
				}
				calls.Reset()
				b.ReportAllocs()
				for b.Loop() {
					if mode == "publish" {
						b.StopTimer()
						if err := base.Get(b.Context(), req.NamespacedName, obj); err != nil {
							b.Fatal(err)
						}
						obj.Generation++
						if err := base.Update(b.Context(), obj); err != nil {
							b.Fatal(err)
						}
						b.StartTimer()
					}
					if _, err := r.Reconcile(b.Context(), req); err != nil {
						b.Fatal(err)
					}
				}
				calls.Report(b)
				if err := base.Get(b.Context(), req.NamespacedName, obj); err != nil {
					b.Fatal(err)
				}
				if len(obj.Status.Rules) != count || obj.Status.ObservedGeneration != obj.Generation || !meta.IsStatusConditionTrue(obj.Status.Conditions, meta.ReadyCondition) {
					b.Fatal("rules were not published")
				}
			})
		}
	}
}
