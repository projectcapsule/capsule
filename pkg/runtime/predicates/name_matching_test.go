// Copyright 2020-2025 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package predicates_test

import (
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/event"

	"github.com/projectcapsule/capsule/pkg/runtime/predicates"
)

func TestNamesMatchingPredicate_Matches(t *testing.T) {
	t.Parallel()

	mk := func(name string) *unstructured.Unstructured {
		u := &unstructured.Unstructured{}
		u.SetAPIVersion("v1")
		u.SetKind("ConfigMap")
		u.SetName(name)
		u.SetNamespace("ns")
		return u
	}

	p := predicates.NamesMatchingPredicate{Names: []string{"a", "b"}}

	t.Run("Create/Delete/Generic match by name", func(t *testing.T) {
		t.Parallel()

		if !p.Create(event.CreateEvent{Object: mk("a")}) {
			t.Fatalf("expected Create match for name a")
		}
		if p.Create(event.CreateEvent{Object: mk("c")}) {
			t.Fatalf("expected no Create match for name c")
		}

		if !p.Delete(event.DeleteEvent{Object: mk("b")}) {
			t.Fatalf("expected Delete match for name b")
		}
		if p.Delete(event.DeleteEvent{Object: mk("c")}) {
			t.Fatalf("expected no Delete match for name c")
		}

		if !p.Generic(event.GenericEvent{Object: mk("a")}) {
			t.Fatalf("expected Generic match for name a")
		}
		if p.Generic(event.GenericEvent{Object: mk("c")}) {
			t.Fatalf("expected no Generic match for name c")
		}
	})

	t.Run("Update checks new object only", func(t *testing.T) {
		t.Parallel()

		// Old matches, new doesn't => false
		if p.Update(event.UpdateEvent{ObjectOld: mk("a"), ObjectNew: mk("c")}) {
			t.Fatalf("expected false when new name does not match")
		}

		// Old doesn't match, new matches => true
		if !p.Update(event.UpdateEvent{ObjectOld: mk("c"), ObjectNew: mk("b")}) {
			t.Fatalf("expected true when new name matches")
		}
	})
}
