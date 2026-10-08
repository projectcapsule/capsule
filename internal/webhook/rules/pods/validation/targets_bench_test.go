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
	"github.com/projectcapsule/capsule/pkg/runtime/handlers"
)

func BenchmarkPodTargetAdmission(b *testing.B) {
	for _, count := range []int{1, 20} {
		b.Run(fmt.Sprintf("rules=%d", count), func(b *testing.B) {
			h := PodRules(nil, nil, nil)
			pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "app", Namespace: "tenant-a"}, Spec: corev1.PodSpec{SchedulerName: "default-scheduler", Containers: []corev1.Container{{Name: "app", Image: "example.com/team/app:v1"}}}}
			var bodies []*rules.NamespaceRuleBodyNamespace
			for range count {
				bodies = append(bodies, &rules.NamespaceRuleBodyNamespace{Enforce: &rules.NamespaceRuleEnforceBody{Workloads: rules.NamespaceRuleEnforceWorkloadsBody{Placement: rules.WorkloadPlacementEnforcement{Schedulers: []apiruntime.ExpressionMatch{{Exact: []string{"blocked"}}}}}}})
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

func BenchmarkSchedulerCompatibilityAdmission(b *testing.B) {
	for _, tenants := range []int{1, 8} {
		for _, count := range []int{1, 20} {
			for _, field := range []string{"placement", "placement-pair", "legacy", "both"} {
				for _, mode := range []string{"allow", "deny", "skip", "cold", "parallel"} {
					b.Run(fmt.Sprintf("tenants=%d/rules=%d/%s/%s", tenants, count, field, mode), func(b *testing.B) {
						h := PodRules(nil, nil, nil).(*podRules)
						recorder := events.NewEventRecorder(nil, logr.Discard(), nil, nil)
						calls := make([]handlers.Func, tenants)
						for i := range tenants {
							name := fmt.Sprintf("tenant-%d", i)
							tnt := &capsulev1beta2.Tenant{ObjectMeta: metav1.ObjectMeta{Name: name}}
							pod := schedulerPodForTest("allowed-" + name)
							pod.Namespace = name
							if mode == "deny" {
								pod.Spec.SchedulerName = "blocked-" + name
							}
							bodies := make([]*rules.NamespaceRuleBodyNamespace, count)
							for j := range bodies {
								body := &rules.NamespaceRuleEnforceBody{Action: rules.ActionTypeDeny}
								match := schedulerExpressionForTest("^blocked-" + name + "$")
								if field != "legacy" {
									body.Workloads.Placement.Schedulers = []apiruntime.ExpressionMatch{match}
								}
								if field == "placement-pair" {
									body.Workloads.Placement.Schedulers = append(body.Workloads.Placement.Schedulers, match)
								}
								if field == "legacy" || field == "both" {
									body.Workloads.Schedulers = []apiruntime.ExpressionMatch{match}
								}
								if mode == "skip" {
									body.Workloads.Targets = []rules.WorkloadValidationTarget{rules.ValidateJob}
								}
								bodies[j] = &rules.NamespaceRuleBodyNamespace{Enforce: body}
							}
							calls[i] = h.OnCreate(nil, nil, pod, nil, recorder, tnt, bodies)
						}
						run := func(i int) {
							response := calls[i%tenants](b.Context(), admission.Request{})
							if (response != nil) != (mode == "deny") || (response != nil && response.Allowed) {
								b.Fatalf("unexpected response: %v", response)
							}
						}
						for i := range tenants {
							run(i)
						}
						b.ReportAllocs()
						b.ResetTimer()
						if mode == "parallel" {
							b.RunParallel(func(pb *testing.PB) {
								for i := 0; pb.Next(); i++ {
									run(i)
								}
							})
						} else {
							for i := 0; b.Loop(); i++ {
								if mode == "cold" {
									h.regexCache.Reset()
								}
								run(i)
							}
						}
					})
				}
			}
		}
	}
}
