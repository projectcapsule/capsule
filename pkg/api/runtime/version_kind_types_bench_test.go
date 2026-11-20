// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package runtime

import (
	"testing"

	"k8s.io/apimachinery/pkg/runtime/schema"
)

// BenchmarkVersionKindMatchesGroupVersionKind measures the GVK matching path
// used by rules admission, including matchAPIGroupPattern and matchPattern.
func BenchmarkVersionKindMatchesGroupVersionKind(b *testing.B) {
	gvk := schema.GroupVersionKind{Group: "apps", Version: "v1", Kind: "Deployment"}

	cases := []struct {
		name string
		vk   VersionKind
		want bool
	}{
		{"exact_match", VersionKind{APIVersion: "apps/v1", Kind: "Deployment"}, true},
		{"wildcard_kind_match", VersionKind{APIVersion: "apps/v1", Kind: "Deploy*"}, true},
		{"wildcard_kind_mismatch", VersionKind{APIVersion: "apps/v1", Kind: "Stateful*"}, false},
		{"wildcard_overlap_reject", VersionKind{APIVersion: "apps/v1", Kind: "Deployment*ment"}, false},
		{"wildcard_multi_match", VersionKind{APIVersion: "apps/v1", Kind: "D*p*m*t"}, true},
		{"wildcard_group_match", VersionKind{APIVersion: "*", Kind: "Deployment"}, true},
	}

	for _, c := range cases {
		b.Run(c.name, func(b *testing.B) {
			if got := c.vk.MatchesGroupVersionKind(gvk); got != c.want {
				b.Fatalf("MatchesGroupVersionKind(%+v) = %t, want %t", c.vk, got, c.want)
			}

			b.ReportAllocs()

			for b.Loop() {
				_ = c.vk.MatchesGroupVersionKind(gvk)
			}
		})
	}
}
