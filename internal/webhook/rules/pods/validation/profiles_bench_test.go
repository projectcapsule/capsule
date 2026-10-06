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

	capsule "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/rules"
	apiruntime "github.com/projectcapsule/capsule/pkg/api/runtime"
	"github.com/projectcapsule/capsule/pkg/runtime/events"
	"github.com/projectcapsule/capsule/pkg/runtime/handlers"
)

func BenchmarkSecurityProfileAdmission(b *testing.B) {
	for _, tenants := range []int{1, 10} {
		for _, size := range []int{1, 10} {
			for _, mode := range []string{"allow-warm", "deny", "skip", "cold", "parallel"} {
				b.Run(fmt.Sprintf("tenants=%d/containers-rules=%d/%s", tenants, size, mode), func(b *testing.B) {
					h := newPodRules(nil, nil, nil)
					recorder := events.NewEventRecorder(nil, logr.Discard(), nil, nil)
					calls := make([]handlers.Func, 0, tenants)
					for ti := range tenants {
						tnt := &capsule.Tenant{ObjectMeta: metav1.ObjectMeta{Name: fmt.Sprintf("tenant-%d", ti)}}
						p := profilePod()
						p.Namespace = tnt.Name
						p.Spec.InitContainers = nil
						p.Spec.EphemeralContainers = nil
						p.Spec.Containers = make([]corev1.Container, size)
						for ci := range size {
							p.Spec.Containers[ci] = corev1.Container{Name: fmt.Sprintf("app-%d", ci), Image: "example.com/app:v1", SecurityContext: profileContext(rules.SecurityProfileLocalhost, tnt.Name+"/app.json")}
						}
						var bodies []*rules.NamespaceRuleBodyNamespace
						for range size {
							body := profileBody(false, rules.ActionTypeAllow, rules.SecurityProfileLocalhost)
							body.Workloads.Security.SeccompProfiles[0].LocalhostProfiles = []apiruntime.ExpressionMatch{{ExpressionRegex: apiruntime.ExpressionRegex{Expression: "^" + tnt.Name + `/.*\.json$`}}}
							body.Workloads.Security.AppArmorProfiles = body.Workloads.Security.SeccompProfiles
							if mode == "skip" {
								body.Workloads.Targets = []rules.WorkloadValidationTarget{rules.ValidateDeployment}
							}
							bodies = append(bodies, &rules.NamespaceRuleBodyNamespace{Enforce: body})
						}
						if mode == "deny" {
							p.Spec.Containers[0].SecurityContext = profileContext(rules.SecurityProfileUnconfined, "")
						}
						calls = append(calls, h.OnCreate(nil, nil, p, nil, recorder, tnt, bodies))
					}
					run := func(i int) {
						if mode == "cold" {
							h.regexCache.Reset()
						}
						response := calls[i%len(calls)](b.Context(), admission.Request{})
						denied := response != nil && !response.Allowed
						if denied != (mode == "deny") {
							b.Fatalf("unexpected response: %+v", response)
						}
					}
					for i := range calls {
						run(i)
					}
					b.ReportAllocs()
					b.ResetTimer()
					if mode == "parallel" {
						b.RunParallel(func(pb *testing.PB) {
							i := 0
							for pb.Next() {
								run(i)
								i++
							}
						})
					} else {
						i := 0
						for b.Loop() {
							run(i)
							i++
						}
					}
				})
			}
		}
	}
}
