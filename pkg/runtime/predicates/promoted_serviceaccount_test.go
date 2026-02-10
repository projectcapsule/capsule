// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package predicates_test

import (
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/event"

	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/runtime/predicates"
)

func TestPromotedServiceaccountPredicate_StaticFuncs(t *testing.T) {
	t.Parallel()

	p := predicates.PromotedServiceaccountPredicate{}

	if got := p.Generic(event.GenericEvent{}); got {
		t.Fatalf("Generic() = %v, want false", got)
	}
}

func TestPromotedServiceaccountPredicate_CreateDelete(t *testing.T) {
	t.Parallel()

	p := predicates.PromotedServiceaccountPredicate{}

	mk := func(lbl map[string]string) *unstructured.Unstructured {
		u := &unstructured.Unstructured{}
		u.SetAPIVersion("v1")
		u.SetKind("ServiceAccount")
		u.SetName("sa")
		u.SetNamespace("ns")
		u.SetLabels(lbl)
		return u
	}

	t.Run("Create returns true only when trigger label present and equals trigger value", func(t *testing.T) {
		t.Parallel()

		if got := p.Create(event.CreateEvent{Object: mk(nil)}); got {
			t.Fatalf("Create() = %v, want false (no labels)", got)
		}

		if got := p.Create(event.CreateEvent{Object: mk(map[string]string{meta.OwnerPromotionLabel: "nope"})}); got {
			t.Fatalf("Create() = %v, want false (wrong value)", got)
		}

		if got := p.Create(event.CreateEvent{Object: mk(map[string]string{meta.OwnerPromotionLabel: meta.ValueTrue})}); !got {
			t.Fatalf("Create() = %v, want true (trigger)", got)
		}
	})

	t.Run("Delete returns true only when trigger label present and equals trigger value", func(t *testing.T) {
		t.Parallel()

		if got := p.Delete(event.DeleteEvent{Object: mk(nil)}); got {
			t.Fatalf("Delete() = %v, want false (no labels)", got)
		}

		if got := p.Delete(event.DeleteEvent{Object: mk(map[string]string{meta.OwnerPromotionLabel: "nope"})}); got {
			t.Fatalf("Delete() = %v, want false (wrong value)", got)
		}

		if got := p.Delete(event.DeleteEvent{Object: mk(map[string]string{meta.OwnerPromotionLabel: meta.ValueTrue})}); !got {
			t.Fatalf("Delete() = %v, want true (trigger)", got)
		}
	})
}

func TestPromotedServiceaccountPredicate_Update(t *testing.T) {
	t.Parallel()

	p := predicates.PromotedServiceaccountPredicate{}

	mk := func(lbl map[string]string) *unstructured.Unstructured {
		u := &unstructured.Unstructured{}
		u.SetAPIVersion("v1")
		u.SetKind("ServiceAccount")
		u.SetName("sa")
		u.SetNamespace("ns")
		u.SetLabels(lbl)
		return u
	}

	tests := []struct {
		name string
		old  map[string]string
		new  map[string]string
		want bool
	}{
		{"no label in either", nil, nil, false},
		{"label added", nil, map[string]string{meta.OwnerPromotionLabel: meta.ValueTrue}, true},
		{"label removed", map[string]string{meta.OwnerPromotionLabel: meta.ValueTrue}, nil, true},
		{"label value changed", map[string]string{meta.OwnerPromotionLabel: "a"}, map[string]string{meta.OwnerPromotionLabel: "b"}, true},
		{"label unchanged", map[string]string{meta.OwnerPromotionLabel: "a"}, map[string]string{meta.OwnerPromotionLabel: "a"}, false},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			ev := event.UpdateEvent{
				ObjectOld: mk(tt.old),
				ObjectNew: mk(tt.new),
			}

			if got := p.Update(ev); got != tt.want {
				t.Fatalf("Update() = %v, want %v", got, tt.want)
			}
		})
	}
}
