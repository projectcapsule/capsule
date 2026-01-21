// Copyright 2020-2025 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package predicates_test

import (
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/event"

	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/runtime/predicates"
)

func TestReconcileRequestedPredicate_StaticFuncs(t *testing.T) {
	t.Parallel()

	p := predicates.ReconcileRequestedPredicate{}

	if got := p.Create(event.CreateEvent{}); got {
		t.Fatalf("Create() = %v, want false", got)
	}
	if got := p.Delete(event.DeleteEvent{}); got {
		t.Fatalf("Delete() = %v, want false", got)
	}
	if got := p.Generic(event.GenericEvent{}); got {
		t.Fatalf("Generic() = %v, want false", got)
	}
}

func TestReconcileRequestedPredicate_Update(t *testing.T) {
	t.Parallel()

	p := predicates.ReconcileRequestedPredicate{}

	mkObj := func(ann map[string]string) *unstructured.Unstructured {
		u := &unstructured.Unstructured{}
		u.SetAPIVersion("capsule.clastix.io/v1beta2")
		u.SetKind("GlobalTenantResource")
		u.SetName("x")

		// Important: nil vs empty map both behave the same for lookups,
		// but we keep this as-is to match real objects.
		u.SetAnnotations(ann)

		return u
	}

	type tc struct {
		name string
		old  map[string]string
		new  map[string]string
		want bool
	}

	tests := []tc{
		{
			name: "nil old object returns false",
			old:  nil, new: map[string]string{meta.ReconcileAnnotation: "2026-01-13T06:23:14+01:00"},
			want: false,
		},
		{
			name: "nil new object returns false",
			old:  map[string]string{meta.ReconcileAnnotation: "2026-01-13T06:23:14+01:00"}, new: nil,
			want: false,
		},
		{
			name: "annotation added triggers true",
			old:  map[string]string{},
			new:  map[string]string{meta.ReconcileAnnotation: "2026-01-13T06:23:14.333872+01:00"},
			want: true,
		},
		{
			name: "annotation value changed triggers true",
			old:  map[string]string{meta.ReconcileAnnotation: "2026-01-13T06:23:14.333872+01:00"},
			new:  map[string]string{meta.ReconcileAnnotation: "2026-01-13T06:24:14.111111+01:00"},
			want: true,
		},
		{
			name: "annotation unchanged does not trigger",
			old:  map[string]string{meta.ReconcileAnnotation: "2026-01-13T06:23:14.333872+01:00"},
			new:  map[string]string{meta.ReconcileAnnotation: "2026-01-13T06:23:14.333872+01:00"},
			want: false,
		},
		{
			name: "annotation removed does not trigger",
			old:  map[string]string{meta.ReconcileAnnotation: "2026-01-13T06:23:14.333872+01:00"},
			new:  map[string]string{}, // removed
			want: false,
		},
		{
			name: "annotation absent in both does not trigger",
			old:  map[string]string{},
			new:  map[string]string{},
			want: false,
		},
		{
			name: "annotation set to empty string does not trigger",
			old:  map[string]string{},
			new:  map[string]string{meta.ReconcileAnnotation: ""},
			want: false,
		},
		{
			name: "annotation changed to empty string (effectively removed) does not trigger",
			old:  map[string]string{meta.ReconcileAnnotation: "2026-01-13T06:23:14.333872+01:00"},
			new:  map[string]string{meta.ReconcileAnnotation: ""},
			want: false,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var oldObj, newObj *unstructured.Unstructured
			// For the nil-object tests, we want ObjectOld/ObjectNew to be nil.
			// Otherwise create real objects.
			if tt.name != "nil old object returns false" {
				oldObj = mkObj(tt.old)
			}
			if tt.name != "nil new object returns false" {
				newObj = mkObj(tt.new)
			}

			ev := event.UpdateEvent{
				ObjectOld: oldObj,
				ObjectNew: newObj,
			}

			got := p.Update(ev)
			if got != tt.want {
				t.Fatalf("Update() = %v, want %v (old=%v new=%v)", got, tt.want, tt.old, tt.new)
			}
		})
	}
}
