// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package validation

import (
	"fmt"
	"testing"

	snapshotv1 "github.com/kubernetes-csi/external-snapshotter/client/v8/apis/volumesnapshot/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	"github.com/projectcapsule/capsule/internal/cache"
	"github.com/projectcapsule/capsule/pkg/api/rules"
)

// benchCase builds the tenants, rules and request of one admission benchmark
// case, and reports whether the request is denied.
func benchCase(b *testing.B, tenants, count int, mode string) (*countingClient, admission.Decoder, admission.Request, bool) {
	b.Helper()

	body := allowNames("silver")
	body.Enforce.Storage.Classes = append(body.Enforce.Storage.Classes, rules.StorageClassMatch{Expression: "^csi-"})

	switch mode {
	case "allow-name-selector", "allow-selector":
		body.Enforce.Storage.Classes = append(body.Enforce.Storage.Classes, customerClasses)
	case "no-class-rules":
		body = &rules.NamespaceRuleBodyNamespace{Enforce: &rules.NamespaceRuleEnforceBody{Action: rules.ActionTypeAllow, Storage: rules.NamespaceRuleEnforceStorageBody{
			Volumes: []rules.PersistentVolumeMatch{{Selector: &metav1.LabelSelector{}}},
		}}}
	}

	bodies := make([]*rules.NamespaceRuleBodyNamespace, count)
	for i := range bodies {
		bodies[i] = body.DeepCopy()
	}

	profiles := []profile{{tenant: "tenant-0", namespace: "tenant-0", rules: bodies, status: true}}
	for i := 1; i < tenants; i++ {
		name := fmt.Sprintf("unrelated-%d", i)
		profiles = append(profiles, profile{tenant: name, namespace: name, rules: []*rules.NamespaceRuleBodyNamespace{denyNames("silver")}, status: true})
	}

	cl, decoder := fixture(b, profiles...)

	class, denied := new("silver"), false
	switch mode {
	case "allow-selector":
		class = new("gold")
	case "deny":
		class, denied = new("bronze"), true
	case "missing-class":
		class, denied = nil, true
	}

	req := request(b, newSnapshot("tenant-0", class), nil)
	switch mode {
	case "unchanged-update":
		req = request(b, newSnapshot("tenant-0", new("bronze"), func(obj *snapshotv1.VolumeSnapshot) { obj.Labels = map[string]string{"retention": "weekly"} }), newSnapshot("tenant-0", new("bronze")))
	case "other-kind":
		req.Kind = metav1.GroupVersionKind{Version: "v1", Kind: "PersistentVolumeClaim"}
	}

	return cl, decoder, req, denied
}

func BenchmarkVolumeSnapshotClassAdmission(b *testing.B) {
	modes := []string{"allow-name", "allow-name-selector", "allow-selector", "deny", "missing-class", "no-class-rules", "unchanged-update", "other-kind"}

	for _, tenants := range []int{1, 100} {
		for _, count := range []int{1, 16} {
			for _, mode := range modes {
				b.Run(fmt.Sprintf("tenants=%d/rules=%d/%s", tenants, count, mode), func(b *testing.B) {
					cl, decoder, req, denied := benchCase(b, tenants, count, mode)

					handler := Handler(nil, cache.NewRegexCache(), cache.NewLabelSelectorCache(), compiler)
					recorder := newRecorder()

					run(b.Context(), handler, cl, decoder, recorder, req)
					cl.gets.Store(0)
					cl.classGets.Store(0)

					b.ReportAllocs()

					for b.Loop() {
						recorder.emitted = recorder.emitted[:0]

						response := run(b.Context(), handler, cl, decoder, recorder, req)
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

// BenchmarkVolumeSnapshotClassAdmissionCaches covers what the warm serial cases
// can't show. Cold builds the handler with empty caches before every request,
// so its expressions and selectors are compiled again. Parallel shares one warm
// handler between goroutines, the way concurrent admissions share its caches.
func BenchmarkVolumeSnapshotClassAdmissionCaches(b *testing.B) {
	for _, tenants := range []int{1, 100} {
		for _, count := range []int{1, 16} {
			for _, mode := range []string{"allow-name", "allow-selector", "deny"} {
				for _, phase := range []string{"cold", "parallel"} {
					b.Run(fmt.Sprintf("tenants=%d/rules=%d/%s/%s", tenants, count, mode, phase), func(b *testing.B) {
						cl, decoder, req, denied := benchCase(b, tenants, count, mode)

						b.ReportAllocs()

						if phase == "cold" {
							for b.Loop() {
								handler := Handler(nil, cache.NewRegexCache(), cache.NewLabelSelectorCache(), compiler)

								response := run(b.Context(), handler, cl, decoder, newRecorder(), req)
								if (response != nil) != denied {
									b.Fatalf("unexpected response: %#v", response)
								}
							}
						} else {
							handler := Handler(nil, cache.NewRegexCache(), cache.NewLabelSelectorCache(), compiler)

							run(b.Context(), handler, cl, decoder, newRecorder(), req)
							cl.gets.Store(0)
							cl.classGets.Store(0)

							b.ResetTimer()
							b.RunParallel(func(pb *testing.PB) {
								recorder := newRecorder()

								for pb.Next() {
									recorder.emitted = recorder.emitted[:0]

									response := run(b.Context(), handler, cl, decoder, recorder, req)
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
