// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package validation

import (
	"fmt"
	"testing"

	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsule "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/internal/cache"
	"github.com/projectcapsule/capsule/internal/webhook/pvc"
	"github.com/projectcapsule/capsule/pkg/api/rules"
)

func BenchmarkAdditionalVolumeAccess(b *testing.B) {
	for _, tenants := range []int{1, 1000} {
		for _, count := range []int{1, 20} {
			for _, mode := range []string{"allow", "deny", "cold"} {
				b.Run(fmt.Sprintf("tenants=%d/rules=%d/%s", tenants, count, mode), func(b *testing.B) {
					f := newVolumeFixture()
					f.status.Status.Rules = make([]*rules.NamespaceRuleBodyNamespace, count)
					for i := range f.status.Status.Rules {
						f.status.Status.Rules[i] = f.body.DeepCopy()
					}
					if mode == "deny" {
						f.pv.Labels["pool"] = "other"
					}
					c := f.client(b)
					for i := 1; i < tenants; i++ {
						if err := c.Create(b.Context(), &capsule.Tenant{Name: fmt.Sprintf("unrelated-%d", i)}); err != nil {
							b.Fatal(err)
						}
					}
					compiler, err := cache.NewCELCache()
					if err != nil {
						b.Fatal(err)
					}
					access := VolumeRules(nil, cache.NewLabelSelectorCache(), compiler)
					handle := pvc.Handler(pvc.PersistentVolumeValidatingVolume(access)).OnUpdate(c, c, admission.NewDecoder(c.Scheme()), nil)
					req := f.request(b)
					handle(b.Context(), req)
					c.gets = map[string]int{}
					b.ReportAllocs()
					for b.Loop() {
						if mode == "cold" {
							compiler.PruneConditions(nil)
						}
						response := handle(b.Context(), req)
						if (response == nil) != (mode != "deny") {
							b.Fatalf("unexpected response: %#v", response)
						}
					}
					b.ReportMetric(float64(c.gets["*v1.PersistentVolume"])/float64(b.N), "pv-reads/op")
					b.ReportMetric(float64(c.gets["*v1beta2.RuleStatus"])/float64(b.N), "rules-reads/op")
				})
			}
		}
	}
}

func BenchmarkAdditionalVolumeAccessParallel(b *testing.B) {
	for _, count := range []int{1, 20} {
		b.Run(fmt.Sprintf("rules=%d", count), func(b *testing.B) {
			f := newVolumeFixture()
			f.status.Status.Rules = make([]*rules.NamespaceRuleBodyNamespace, count)
			for i := range f.status.Status.Rules {
				f.status.Status.Rules[i] = f.body.DeepCopy()
			}
			// The counting test wrapper is request-local. Use its concurrency-safe
			// underlying fake client for simultaneous admissions.
			c := f.client(b).Client
			compiler, err := cache.NewCELCache()
			if err != nil {
				b.Fatal(err)
			}
			access := VolumeRules(nil, cache.NewLabelSelectorCache(), compiler)
			handle := pvc.Handler(pvc.PersistentVolumeValidatingVolume(access)).OnUpdate(c, c, admission.NewDecoder(c.Scheme()), nil)
			req := f.request(b)
			if response := handle(b.Context(), req); response != nil {
				b.Fatalf("unexpected response: %#v", response)
			}
			b.ReportAllocs()
			b.ResetTimer()
			b.RunParallel(func(pb *testing.PB) {
				for pb.Next() {
					if response := handle(b.Context(), req); response != nil {
						b.Errorf("unexpected response: %#v", response)
					}
				}
			})
		})
	}
}
