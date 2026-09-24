// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package api_test

import (
	"testing"

	"github.com/projectcapsule/capsule/pkg/api"
)

func BenchmarkAllowedListSpec_Match(b *testing.B) {
	exactSpec := api.AllowedListSpec{
		Exact: []string{"gold", "silver", "bronze", "platinum", "titanium"},
	}
	regexSpec := api.AllowedListSpec{
		Regex: "^(gold|silver|bronze|platinum|titanium)-.*$",
	}
	invalidRegexSpec := api.AllowedListSpec{
		Regex: "[",
	}

	b.Run("exact_hit", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			_ = exactSpec.Match("platinum")
		}
	})

	b.Run("exact_miss", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			_ = exactSpec.Match("nonexistent")
		}
	})

	b.Run("regex_hit", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			_ = regexSpec.Match("platinum-v1")
		}
	})

	b.Run("regex_miss", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			_ = regexSpec.Match("nonexistent-v1")
		}
	})

	b.Run("invalid_regex", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			_ = invalidRegexSpec.Match("any-value")
		}
	})
}
