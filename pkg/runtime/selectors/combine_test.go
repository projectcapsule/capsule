// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package selectors_test

import (
	"testing"

	"github.com/projectcapsule/capsule/pkg/runtime/selectors"
	"k8s.io/apimachinery/pkg/labels"
)

func TestCombineSelectors(t *testing.T) {
	t.Parallel()

	t.Run("no selectors returns Everything (matches all)", func(t *testing.T) {
		t.Parallel()

		sel := selectors.CombineSelectors()
		if !sel.Matches(labels.Set{}) {
			t.Fatalf("expected combined selector to match empty label set")
		}
		// labels.NewSelector() string is typically "", which means "everything"
		if got := sel.String(); got != "" {
			t.Fatalf("expected empty selector string, got %q", got)
		}
	})

	t.Run("nil selectors are ignored", func(t *testing.T) {
		t.Parallel()

		base := labels.SelectorFromSet(labels.Set{"a": "1"})
		sel := selectors.CombineSelectors(nil, base, nil)

		if !sel.Matches(labels.Set{"a": "1"}) {
			t.Fatalf("expected to match labels a=1")
		}
		if sel.Matches(labels.Set{"a": "2"}) {
			t.Fatalf("expected not to match labels a=2")
		}
	})

	t.Run("combines selectors with AND semantics", func(t *testing.T) {
		t.Parallel()

		s1 := labels.SelectorFromSet(labels.Set{"a": "1"})
		s2 := labels.SelectorFromSet(labels.Set{"b": "2"})

		combined := selectors.CombineSelectors(s1, s2)

		if !combined.Matches(labels.Set{"a": "1", "b": "2"}) {
			t.Fatalf("expected to match when both requirements are satisfied")
		}
		if combined.Matches(labels.Set{"a": "1"}) {
			t.Fatalf("expected not to match when b is missing")
		}
		if combined.Matches(labels.Set{"b": "2"}) {
			t.Fatalf("expected not to match when a is missing")
		}
		if combined.Matches(labels.Set{"a": "1", "b": "3"}) {
			t.Fatalf("expected not to match when b mismatches")
		}
	})

	t.Run("conflicting selectors match nothing", func(t *testing.T) {
		t.Parallel()

		s1 := labels.SelectorFromSet(labels.Set{"a": "1"})
		s2 := labels.SelectorFromSet(labels.Set{"a": "2"})

		combined := selectors.CombineSelectors(s1, s2)

		if combined.Matches(labels.Set{"a": "1"}) {
			t.Fatalf("expected not to match due to conflict (a=1 AND a=2)")
		}
		if combined.Matches(labels.Set{"a": "2"}) {
			t.Fatalf("expected not to match due to conflict (a=1 AND a=2)")
		}
	})

	t.Run("non-selectable selector returns Nothing", func(t *testing.T) {
		t.Parallel()

		// labels.Nothing() is not selectable (Requirements() => selectable=false).
		combined := selectors.CombineSelectors(labels.SelectorFromSet(labels.Set{"a": "1"}), labels.Nothing())

		if combined.String() != labels.Nothing().String() {
			t.Fatalf("expected labels.Nothing() selector, got %q", combined.String())
		}
		if combined.Matches(labels.Set{"a": "1"}) {
			t.Fatalf("expected Nothing selector to match nothing")
		}
		if combined.Matches(labels.Set{}) {
			t.Fatalf("expected Nothing selector to match nothing (even empty set)")
		}
	})

	t.Run("output selector is independent from input mutation patterns", func(t *testing.T) {
		t.Parallel()

		// This is a light regression guard: we depend on CombineSelectors turning input
		// selectors into requirements, not keeping references to the original selector objects.
		in := labels.SelectorFromSet(labels.Set{"a": "1"})
		out := selectors.CombineSelectors(in)

		if !out.Matches(labels.Set{"a": "1"}) {
			t.Fatalf("expected out to match a=1")
		}
		if out.Matches(labels.Set{"a": "2"}) {
			t.Fatalf("expected out not to match a=2")
		}
	})
}
