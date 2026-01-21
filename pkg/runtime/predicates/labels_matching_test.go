// Copyright 2020-2025 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package predicates_test

import (
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/event"

	"github.com/projectcapsule/capsule/pkg/runtime/predicates"
)

func TestLabelsMatchingPredicate_Matches(t *testing.T) {
	t.Parallel()

	mk := func(lbl map[string]string) *unstructured.Unstructured {
		u := &unstructured.Unstructured{}
		u.SetAPIVersion("v1")
		u.SetKind("ConfigMap")
		u.SetName("cm")
		u.SetNamespace("ns")
		u.SetLabels(lbl)
		return u
	}

	t.Run("empty match map matches everything (including nil labels)", func(t *testing.T) {
		t.Parallel()

		p := predicates.LabelsMatchingPredicate{Match: map[string]string{}}

		if !p.Create(event.CreateEvent{Object: mk(nil)}) {
			t.Fatalf("Create should match when Match is empty")
		}
		if !p.Update(event.UpdateEvent{ObjectNew: mk(nil)}) {
			t.Fatalf("Update should match when Match is empty")
		}
		if !p.Delete(event.DeleteEvent{Object: mk(nil)}) {
			t.Fatalf("Delete should match when Match is empty")
		}
		if !p.Generic(event.GenericEvent{Object: mk(nil)}) {
			t.Fatalf("Generic should match when Match is empty")
		}
	})

	t.Run("non-empty match requires all key/value pairs", func(t *testing.T) {
		t.Parallel()

		p := predicates.LabelsMatchingPredicate{Match: map[string]string{"app": "x", "tier": "backend"}}

		// Missing labels
		if p.Create(event.CreateEvent{Object: mk(nil)}) {
			t.Fatalf("expected no match when labels are nil")
		}

		// Partial match
		if p.Create(event.CreateEvent{Object: mk(map[string]string{"app": "x"})}) {
			t.Fatalf("expected no match when one label missing")
		}

		// Wrong value
		if p.Create(event.CreateEvent{Object: mk(map[string]string{"app": "x", "tier": "frontend"})}) {
			t.Fatalf("expected no match when value differs")
		}

		// Full match
		if !p.Create(event.CreateEvent{Object: mk(map[string]string{"app": "x", "tier": "backend"})}) {
			t.Fatalf("expected match when all labels match")
		}
	})

	t.Run("Update checks new object only", func(t *testing.T) {
		t.Parallel()

		p := predicates.LabelsMatchingPredicate{Match: map[string]string{"app": "x"}}

		// Old matches, new doesn't => false
		if p.Update(event.UpdateEvent{ObjectOld: mk(map[string]string{"app": "x"}), ObjectNew: mk(map[string]string{"app": "y"})}) {
			t.Fatalf("expected false when new object does not match")
		}

		// Old doesn't match, new matches => true
		if !p.Update(event.UpdateEvent{ObjectOld: mk(map[string]string{"app": "y"}), ObjectNew: mk(map[string]string{"app": "x"})}) {
			t.Fatalf("expected true when new object matches")
		}
	})
}
