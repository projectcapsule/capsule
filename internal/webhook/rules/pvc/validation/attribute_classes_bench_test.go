// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package validation

import (
	"fmt"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	"github.com/projectcapsule/capsule/internal/cache"
	"github.com/projectcapsule/capsule/pkg/api/rules"
)

// classBenchCase builds the tenants, rules and request of one admission
// benchmark case, and reports whether the request is denied.
func classBenchCase(b *testing.B, tenants, count int, mode string) (*classClient, admission.Decoder, admission.Request, bool) {
	b.Helper()

	body := allowClassNames("silver")
	body.Enforce.Storage.AttributeClasses = append(body.Enforce.Storage.AttributeClasses, rules.StorageClassMatch{Expression: "^csi-"})

	switch mode {
	case "allow-name-selector", "allow-selector":
		body.Enforce.Storage.AttributeClasses = append(body.Enforce.Storage.AttributeClasses, customerClasses)
	case "no-class-rules":
		body = &rules.NamespaceRuleBodyNamespace{Enforce: &rules.NamespaceRuleEnforceBody{Action: rules.ActionTypeAllow, Storage: rules.NamespaceRuleEnforceStorageBody{
			Volumes: []rules.PersistentVolumeMatch{{Selector: &metav1.LabelSelector{}}},
		}}}
	}

	bodies := make([]*rules.NamespaceRuleBodyNamespace, count)
	for i := range bodies {
		bodies[i] = body.DeepCopy()
	}

	profiles := []classProfile{{tenant: "tenant-0", namespace: "tenant-0", rules: bodies, status: true}}
	for i := 1; i < tenants; i++ {
		name := fmt.Sprintf("unrelated-%d", i)
		profiles = append(profiles, classProfile{tenant: name, namespace: name, rules: []*rules.NamespaceRuleBodyNamespace{denyClassNames("silver")}, status: true})
	}

	cl, decoder := classFixture(b, profiles...)

	class, denied := new("silver"), false
	switch mode {
	case "allow-selector":
		class = new("gold")
	case "deny":
		class, denied = new("bronze"), true
	case "no-class":
		class = nil
	}

	req := claimRequest(b, newClaim("tenant-0", class), nil)
	switch mode {
	case "unchanged-update":
		req = claimRequest(b, newClaim("tenant-0", new("bronze"), bound(new("bronze")), func(obj *corev1.PersistentVolumeClaim) { obj.Labels = map[string]string{"retention": "weekly"} }), newClaim("tenant-0", new("bronze"), bound(new("bronze"))))
	case "restore-applied":
		req = claimRequest(b, newClaim("tenant-0", new("bronze"), bound(new("bronze"))), newClaim("tenant-0", new("silver"), bound(new("bronze"))))
	case "other-kind":
		req.Kind = metav1.GroupVersionKind{Group: "snapshot.storage.k8s.io", Version: "v1", Kind: "VolumeSnapshot"}
	}

	return cl, decoder, req, denied
}

func BenchmarkVolumeAttributesClassAdmission(b *testing.B) {
	modes := []string{"allow-name", "allow-name-selector", "allow-selector", "deny", "no-class", "no-class-rules", "unchanged-update", "restore-applied", "other-kind"}

	for _, tenants := range []int{1, 100} {
		for _, count := range []int{1, 16} {
			for _, mode := range modes {
				b.Run(fmt.Sprintf("tenants=%d/rules=%d/%s", tenants, count, mode), func(b *testing.B) {
					cl, decoder, req, denied := classBenchCase(b, tenants, count, mode)

					handler := AttributeClassHandler(nil, cache.NewRegexCache(), cache.NewLabelSelectorCache(), classCompiler)
					recorder := newRecorder()

					runClaim(b.Context(), handler, cl, decoder, recorder, req)
					cl.gets.Store(0)
					cl.classGets.Store(0)

					b.ReportAllocs()

					for b.Loop() {
						recorder.emitted = recorder.emitted[:0]

						response := runClaim(b.Context(), handler, cl, decoder, recorder, req)
						if (response != nil) != denied {
							b.Fatalf("unexpected response: %#v", response)
						}
					}

					b.ReportMetric(float64(cl.gets.Load())/float64(b.N), "reads/op")
					b.ReportMetric(float64(cl.classGets.Load())/float64(b.N), "class-reads/op")
				})
			}
		}
	}
}

// BenchmarkVolumeAttributesClassAdmissionCaches covers what the warm serial
// cases can't show. Cold builds the handler with empty caches before every
// request, so its expressions and selectors are compiled again. Parallel shares
// one warm handler between goroutines, the way concurrent admissions share its
// caches.
func BenchmarkVolumeAttributesClassAdmissionCaches(b *testing.B) {
	for _, tenants := range []int{1, 100} {
		for _, count := range []int{1, 16} {
			for _, mode := range []string{"allow-name", "allow-selector", "deny"} {
				for _, phase := range []string{"cold", "parallel"} {
					b.Run(fmt.Sprintf("tenants=%d/rules=%d/%s/%s", tenants, count, mode, phase), func(b *testing.B) {
						cl, decoder, req, denied := classBenchCase(b, tenants, count, mode)

						b.ReportAllocs()

						if phase == "cold" {
							for b.Loop() {
								handler := AttributeClassHandler(nil, cache.NewRegexCache(), cache.NewLabelSelectorCache(), classCompiler)

								response := runClaim(b.Context(), handler, cl, decoder, newRecorder(), req)
								if (response != nil) != denied {
									b.Fatalf("unexpected response: %#v", response)
								}
							}
						} else {
							handler := AttributeClassHandler(nil, cache.NewRegexCache(), cache.NewLabelSelectorCache(), classCompiler)

							runClaim(b.Context(), handler, cl, decoder, newRecorder(), req)
							cl.gets.Store(0)
							cl.classGets.Store(0)

							b.ResetTimer()
							b.RunParallel(func(pb *testing.PB) {
								recorder := newRecorder()

								for pb.Next() {
									recorder.emitted = recorder.emitted[:0]

									response := runClaim(b.Context(), handler, cl, decoder, recorder, req)
									if (response != nil) != denied {
										b.Errorf("unexpected response: %#v", response)
									}
								}
							})
						}

						b.ReportMetric(float64(cl.gets.Load())/float64(b.N), "reads/op")
						b.ReportMetric(float64(cl.classGets.Load())/float64(b.N), "class-reads/op")
					})
				}
			}
		}
	}
}
