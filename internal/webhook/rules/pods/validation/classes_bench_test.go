// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package validation

import (
	"fmt"
	"testing"

	"github.com/go-logr/logr"
	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/rules"
	apiruntime "github.com/projectcapsule/capsule/pkg/api/runtime"
	"github.com/projectcapsule/capsule/pkg/runtime/events"
	"github.com/projectcapsule/capsule/pkg/runtime/handlers"
)

// classBench builds one admission per tenant. Every tenant has its own classes
// and rules, so a decision can never be served from another tenant. The
// returned function binds the admission of a tenant to a handler.
func classBench(b *testing.B, kind classKind, tenants, count int, mode string) (*classReads, func(handlers.TypedHandlerWithTenantWithRuleset[*corev1.Pod], int) handlers.Func, bool) {
	b.Helper()

	var objects []client.Object

	for i := range tenants {
		objects = append(objects,
			kind.class(fmt.Sprintf("tenant-%d-gold", i), map[string]string{"env": "customer"}),
			kind.class(fmt.Sprintf("tenant-%d-bronze", i), map[string]string{"env": "internal"}),
		)
	}

	reader := &classReads{Reader: fake.NewClientBuilder().WithScheme(classScheme(b)).WithObjects(objects...).Build()}
	recorder := events.NewEventRecorder(nil, logr.Discard(), nil, nil)
	calls := make([]func(handlers.TypedHandlerWithTenantWithRuleset[*corev1.Pod]) handlers.Func, tenants)
	denied := mode == "deny-name" || mode == "deny-selector" || mode == "allow-miss"

	for i := range tenants {
		name := fmt.Sprintf("tenant-%d", i)
		silver := classNames(name + "-silver")
		gold := rules.WorkloadClassMatch{Expression: "^" + name + "-(gold|platinum)$"}

		var body *rules.NamespaceRuleBodyNamespace

		class := name + "-gold"

		switch mode {
		case "allow-name", "no-class", "unchanged-update":
			body = kind.body(rules.ActionTypeAllow, silver, gold)
		case "allow-name-selector":
			body = kind.body(rules.ActionTypeAllow, classLabels("env", "internal"), silver, gold)
		case "allow-selector":
			body = kind.body(rules.ActionTypeAllow, silver, classLabels("env", "customer"))
		case "allow-miss":
			body, class = kind.body(rules.ActionTypeAllow, silver, classLabels("env", "customer")), name+"-bronze"
		case "deny-name":
			body = kind.body(rules.ActionTypeDeny, silver, gold)
		case "deny-selector":
			body = kind.body(rules.ActionTypeDeny, silver, classLabels("env", "customer"))
		case "no-class-rules":
			body = &rules.NamespaceRuleBodyNamespace{Enforce: &rules.NamespaceRuleEnforceBody{Workloads: rules.NamespaceRuleEnforceWorkloadsBody{Placement: rules.WorkloadPlacementEnforcement{Schedulers: []apiruntime.ExpressionMatch{{Exact: []string{"blocked"}}}}}}}
		default:
			b.Fatalf("unknown mode %q", mode)
		}

		if mode == "no-class" {
			class = ""
		}

		bodies := make([]*rules.NamespaceRuleBodyNamespace, count)
		for j := range bodies {
			bodies[j] = body.DeepCopy()
		}

		pod := kind.pod(class)
		pod.Namespace = name
		tnt := &capsulev1beta2.Tenant{Name: name}

		if mode == "unchanged-update" {
			// The class is forbidden now, but an update that keeps it is not checked.
			kind.assign(pod, name+"-bronze")
			old := pod.DeepCopy()
			calls[i] = func(h handlers.TypedHandlerWithTenantWithRuleset[*corev1.Pod]) handlers.Func {
				return h.OnUpdate(nil, reader, old, pod, nil, recorder, tnt, bodies)
			}
		} else {
			calls[i] = func(h handlers.TypedHandlerWithTenantWithRuleset[*corev1.Pod]) handlers.Func {
				return h.OnCreate(nil, reader, pod, nil, recorder, tnt, bodies)
			}
		}
	}

	return reader, func(h handlers.TypedHandlerWithTenantWithRuleset[*corev1.Pod], tenant int) handlers.Func {
		return calls[tenant%tenants](h)
	}, denied
}

var classBenchModes = []string{"allow-name", "allow-name-selector", "allow-selector", "allow-miss", "deny-name", "deny-selector", "no-class", "no-class-rules", "unchanged-update"}

func BenchmarkPriorityClassAdmission(b *testing.B) { benchmarkClassAdmission(b, classKinds[0]) }

func BenchmarkRuntimeClassAdmission(b *testing.B) { benchmarkClassAdmission(b, classKinds[1]) }

func benchmarkClassAdmission(b *testing.B, kind classKind) {
	b.Helper()

	for _, tenants := range []int{1, 8} {
		for _, count := range []int{1, 20} {
			for _, mode := range classBenchModes {
				b.Run(fmt.Sprintf("tenants=%d/rules=%d/%s", tenants, count, mode), func(b *testing.B) {
					reader, bind, denied := classBench(b, kind, tenants, count, mode)
					h := PodRules(nil, nil, nil, nil)
					req := admission.Request{}

					if mode == "unchanged-update" {
						req.Operation = admissionv1.Update
					}

					calls := make([]handlers.Func, tenants)
					for i := range tenants {
						calls[i] = bind(h, i)
						calls[i](b.Context(), req)
					}

					reader.gets.Store(0)
					b.ReportAllocs()

					for i := 0; b.Loop(); i++ {
						response := calls[i%tenants](b.Context(), req)
						if (response != nil) != denied || (response != nil && response.Allowed) {
							b.Fatalf("unexpected response: %v", response)
						}
					}

					b.ReportMetric(float64(reader.gets.Load())/float64(b.N), "class-reads/op")
				})
			}
		}
	}
}

func BenchmarkPriorityClassAdmissionCaches(b *testing.B) {
	benchmarkClassAdmissionCaches(b, classKinds[0])
}

func BenchmarkRuntimeClassAdmissionCaches(b *testing.B) {
	benchmarkClassAdmissionCaches(b, classKinds[1])
}

// benchmarkClassAdmissionCaches covers what the warm serial cases can't show.
// Cold builds the handler with empty caches before every request, so its
// expressions and selectors are compiled again. Parallel shares one warm
// handler between goroutines, the way concurrent admissions share its caches.
func benchmarkClassAdmissionCaches(b *testing.B, kind classKind) {
	b.Helper()

	for _, tenants := range []int{1, 8} {
		for _, count := range []int{1, 20} {
			for _, mode := range []string{"allow-name", "allow-selector", "deny-name"} {
				for _, phase := range []string{"cold", "parallel"} {
					b.Run(fmt.Sprintf("tenants=%d/rules=%d/%s/%s", tenants, count, mode, phase), func(b *testing.B) {
						reader, bind, denied := classBench(b, kind, tenants, count, mode)

						b.ReportAllocs()

						if phase == "cold" {
							for i := 0; b.Loop(); i++ {
								response := bind(PodRules(nil, nil, nil, nil), i)(b.Context(), admission.Request{})
								if (response != nil) != denied {
									b.Fatalf("unexpected response: %v", response)
								}
							}
						} else {
							h := PodRules(nil, nil, nil, nil)

							calls := make([]handlers.Func, tenants)
							for i := range tenants {
								calls[i] = bind(h, i)
								calls[i](b.Context(), admission.Request{})
							}

							reader.gets.Store(0)
							b.ResetTimer()
							b.RunParallel(func(pb *testing.PB) {
								for i := 0; pb.Next(); i++ {
									response := calls[i%tenants](b.Context(), admission.Request{})
									if (response != nil) != denied {
										b.Errorf("unexpected response: %v", response)
									}
								}
							})
						}

						b.ReportMetric(float64(reader.gets.Load())/float64(b.N), "class-reads/op")
					})
				}
			}
		}
	}
}

// BenchmarkWorkloadClassKindsAdmission admits a Pod that names a class of every
// kind against rules that carry a list for each of them. Each kind is matched
// on its own, so the cost and the class reads add up.
func BenchmarkWorkloadClassKindsAdmission(b *testing.B) {
	for _, tenants := range []int{1, 8} {
		for _, count := range []int{1, 20} {
			for _, mode := range []string{"allow-name", "allow-selector"} {
				b.Run(fmt.Sprintf("tenants=%d/rules=%d/%s", tenants, count, mode), func(b *testing.B) {
					var objects []client.Object

					calls := make([]handlers.Func, tenants)
					reader := &classReads{}
					h := PodRules(nil, nil, nil, nil)

					for i := range tenants {
						name := fmt.Sprintf("tenant-%d", i)
						match := rules.WorkloadClassMatch{Expression: "^" + name + "-(gold|platinum)$"}

						if mode == "allow-selector" {
							match = classLabels("tenant", name)
						}

						body := &rules.NamespaceRuleBodyNamespace{Enforce: &rules.NamespaceRuleEnforceBody{Action: rules.ActionTypeAllow}}
						pod := classKinds[0].pod("")
						pod.Namespace = name

						for _, kind := range classKinds {
							objects = append(objects, kind.class(name+"-gold", map[string]string{"tenant": name}))
							kind.rule(&body.Enforce.Workloads.Classes, []rules.WorkloadClassMatch{classNames(name + "-silver"), match})
							kind.assign(pod, name+"-gold")
						}

						bodies := make([]*rules.NamespaceRuleBodyNamespace, count)
						for j := range bodies {
							bodies[j] = body.DeepCopy()
						}

						calls[i] = h.OnCreate(nil, reader, pod, nil, events.NewDiscardRecorder(), &capsulev1beta2.Tenant{Name: name}, bodies)
					}

					reader.Reader = fake.NewClientBuilder().WithScheme(classScheme(b)).WithObjects(objects...).Build()

					for _, call := range calls {
						if response := call(b.Context(), admission.Request{}); response != nil {
							b.Fatalf("unexpected response: %v", response)
						}
					}

					reader.gets.Store(0)
					b.ReportAllocs()

					for i := 0; b.Loop(); i++ {
						if response := calls[i%tenants](b.Context(), admission.Request{}); response != nil {
							b.Fatalf("unexpected response: %v", response)
						}
					}

					b.ReportMetric(float64(reader.gets.Load())/float64(b.N), "class-reads/op")
				})
			}
		}
	}
}
