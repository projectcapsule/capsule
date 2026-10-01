// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package validation

import (
	"fmt"
	"testing"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/rules"
	apiruntime "github.com/projectcapsule/capsule/pkg/api/runtime"
	"github.com/projectcapsule/capsule/pkg/runtime/events"
)

func BenchmarkPodTargetAdmission(b *testing.B) {
	for _, count := range []int{1, 20} {
		b.Run(fmt.Sprintf("rules=%d", count), func(b *testing.B) {
			h := PodRules(nil, nil, nil)
			pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "app", Namespace: "tenant-a"}, Spec: corev1.PodSpec{SchedulerName: "default-scheduler", Containers: []corev1.Container{{Name: "app", Image: "example.com/team/app:v1"}}}}
			var bodies []*rules.NamespaceRuleBodyNamespace
			for range count {
				bodies = append(bodies, &rules.NamespaceRuleBodyNamespace{Enforce: &rules.NamespaceRuleEnforceBody{Workloads: rules.NamespaceRuleEnforceWorkloadsBody{Schedulers: []apiruntime.ExpressionMatch{{Exact: []string{"blocked"}}}}}})
			}
			call := h.OnCreate(nil, nil, pod, nil, events.NewEventRecorder(nil, logr.Discard(), nil, nil), &capsulev1beta2.Tenant{}, bodies)
			b.ReportAllocs()
			for b.Loop() {
				if got := call(b.Context(), admission.Request{}); got != nil {
					b.Fatalf("unexpected denial: %v", got)
				}
			}
		})
	}
}
