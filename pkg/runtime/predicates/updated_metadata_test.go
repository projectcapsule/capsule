// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package predicates_test

import (
	"testing"

	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/event"

	"github.com/projectcapsule/capsule/pkg/runtime/predicates"
)

func TestUpdatedMetadataPredicate_StaticEvents(t *testing.T) {
	g := NewWithT(t)
	p := predicates.UpdatedMetadataPredicate{}

	g.Expect(p.Create(event.CreateEvent{})).To(BeTrue())
	g.Expect(p.Delete(event.DeleteEvent{})).To(BeTrue())
	g.Expect(p.Generic(event.GenericEvent{})).To(BeFalse())
}

func TestUpdatedMetadataPredicate_Update(t *testing.T) {
	type tc struct {
		name string
		old  *corev1.Pod
		new  *corev1.Pod
		want bool
	}

	// Helper to build pods with labels/annotations
	pod := func(labels, ann map[string]string) *corev1.Pod {
		return &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Namespace:   "default",
				Name:        "p",
				Labels:      labels,
				Annotations: ann,
			},
		}
	}

	tests := []tc{
		{
			name: "labels changed => true (even if annotations unchanged)",
			old:  pod(map[string]string{"a": "1"}, map[string]string{"x": "1"}),
			new:  pod(map[string]string{"a": "2"}, map[string]string{"x": "1"}),
			want: true,
		},
		{
			name: "labels added => true",
			old:  pod(map[string]string{"a": "1"}, nil),
			new:  pod(map[string]string{"a": "1", "b": "2"}, nil),
			want: true,
		},
		{
			name: "labels removed => true",
			old:  pod(map[string]string{"a": "1", "b": "2"}, nil),
			new:  pod(map[string]string{"a": "1"}, nil),
			want: true,
		},
		{
			name: "labels same, annotations changed => true",
			old:  pod(map[string]string{"a": "1"}, map[string]string{"x": "1"}),
			new:  pod(map[string]string{"a": "1"}, map[string]string{"x": "2"}),
			want: true,
		},
		{
			name: "labels same, annotations added => true",
			old:  pod(map[string]string{"a": "1"}, nil),
			new:  pod(map[string]string{"a": "1"}, map[string]string{"x": "1"}),
			want: true,
		},
		{
			name: "labels same, annotations removed => true",
			old:  pod(map[string]string{"a": "1"}, map[string]string{"x": "1"}),
			new:  pod(map[string]string{"a": "1"}, nil),
			want: true,
		},
		{
			name: "labels and annotations unchanged => false",
			old:  pod(map[string]string{"a": "1"}, map[string]string{"x": "1"}),
			new:  pod(map[string]string{"a": "1"}, map[string]string{"x": "1"}),
			want: false,
		},

		// These two cases depend on your utils.MapEqual semantics:
		// Some implementations consider nil and empty maps equal.
		// If MapEqual treats nil != empty, then these should be true.
		// If MapEqual treats nil == empty, then these should be false.
		{
			name: "nil labels vs empty labels (depends on utils.MapEqual)",
			old:  pod(nil, map[string]string{"x": "1"}),
			new:  pod(map[string]string{}, map[string]string{"x": "1"}),
			want: mapEqualNilEmptyBehavior(),
		},
		{
			name: "nil annotations vs empty annotations (depends on utils.MapEqual)",
			old:  pod(map[string]string{"a": "1"}, nil),
			new:  pod(map[string]string{"a": "1"}, map[string]string{}),
			want: mapEqualNilEmptyBehavior(),
		},
	}

	p := predicates.UpdatedMetadataPredicate{}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)

			ev := event.UpdateEvent{
				ObjectOld: tt.old,
				ObjectNew: tt.new,
			}

			got := p.Update(ev)
			g.Expect(got).To(Equal(tt.want))
		})
	}
}

// mapEqualNilEmptyBehavior returns the expected result for cases where one map is nil and the other is empty.
// If your utils.MapEqual treats nil and empty as equal, expected is false (no change).
// If it treats them as different, expected is true (change detected).
//
// Update this helper to match your actual utils.MapEqual behavior, or (better) directly test utils.MapEqual in its own unit tests.
func mapEqualNilEmptyBehavior() bool {
	// Default assumption: nil and empty are equal => no change => false.
	// If your MapEqual treats them differently, change to true.
	return false
}
