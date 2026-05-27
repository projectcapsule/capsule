// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package predicates_test

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/event"

	"github.com/projectcapsule/capsule/pkg/runtime/predicates"
)

func TestLabelPresentPredicate_Create(t *testing.T) {
	t.Parallel()

	p := predicates.LabelPresentPredicate{Label: "example.com/watch"}

	tests := []struct {
		name  string
		event event.CreateEvent
		want  bool
	}{
		{
			name:  "nil object",
			event: event.CreateEvent{},
			want:  false,
		},
		{
			name: "label present",
			event: event.CreateEvent{
				Object: &corev1.Namespace{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test",
						Labels: map[string]string{
							"example.com/watch": "true",
						},
					},
				},
			},
			want: true,
		},
		{
			name: "label missing",
			event: event.CreateEvent{
				Object: &corev1.Namespace{
					ObjectMeta: metav1.ObjectMeta{
						Name:   "test",
						Labels: map[string]string{"other": "value"},
					},
				},
			},
			want: false,
		},
		{
			name: "labels map nil",
			event: event.CreateEvent{
				Object: &corev1.Namespace{},
			},
			want: false,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := p.Create(tt.event)
			if got != tt.want {
				t.Fatalf("Create() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestLabelPresentPredicate_Delete(t *testing.T) {
	t.Parallel()

	p := predicates.LabelPresentPredicate{Label: "example.com/watch"}

	tests := []struct {
		name  string
		event event.DeleteEvent
		want  bool
	}{
		{
			name:  "nil object",
			event: event.DeleteEvent{},
			want:  false,
		},
		{
			name: "label present",
			event: event.DeleteEvent{
				Object: &corev1.Namespace{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test",
						Labels: map[string]string{
							"example.com/watch": "true",
						},
					},
				},
			},
			want: true,
		},
		{
			name: "label missing",
			event: event.DeleteEvent{
				Object: &corev1.Namespace{
					ObjectMeta: metav1.ObjectMeta{
						Name:   "test",
						Labels: map[string]string{"other": "value"},
					},
				},
			},
			want: false,
		},
		{
			name: "labels map nil",
			event: event.DeleteEvent{
				Object: &corev1.Namespace{},
			},
			want: false,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := p.Delete(tt.event)
			if got != tt.want {
				t.Fatalf("Delete() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestLabelPresentPredicate_Update(t *testing.T) {
	t.Parallel()

	p := predicates.LabelPresentPredicate{Label: "example.com/watch"}

	tests := []struct {
		name  string
		event event.UpdateEvent
		want  bool
	}{
		{
			name:  "nil old object",
			event: event.UpdateEvent{ObjectNew: &corev1.Namespace{}},
			want:  false,
		},
		{
			name:  "nil new object",
			event: event.UpdateEvent{ObjectOld: &corev1.Namespace{}},
			want:  false,
		},
		{
			name:  "both objects nil",
			event: event.UpdateEvent{},
			want:  false,
		},
		{
			name: "label unchanged and present",
			event: event.UpdateEvent{
				ObjectOld: &corev1.Namespace{
					ObjectMeta: metav1.ObjectMeta{
						Labels: map[string]string{
							"example.com/watch": "true",
						},
					},
				},
				ObjectNew: &corev1.Namespace{
					ObjectMeta: metav1.ObjectMeta{
						Labels: map[string]string{
							"example.com/watch": "true",
						},
					},
				},
			},
			want: false,
		},
		{
			name: "label changed value",
			event: event.UpdateEvent{
				ObjectOld: &corev1.Namespace{
					ObjectMeta: metav1.ObjectMeta{
						Labels: map[string]string{
							"example.com/watch": "old",
						},
					},
				},
				ObjectNew: &corev1.Namespace{
					ObjectMeta: metav1.ObjectMeta{
						Labels: map[string]string{
							"example.com/watch": "new",
						},
					},
				},
			},
			want: true,
		},
		{
			name: "label added",
			event: event.UpdateEvent{
				ObjectOld: &corev1.Namespace{
					ObjectMeta: metav1.ObjectMeta{
						Labels: map[string]string{
							"other": "value",
						},
					},
				},
				ObjectNew: &corev1.Namespace{
					ObjectMeta: metav1.ObjectMeta{
						Labels: map[string]string{
							"example.com/watch": "true",
						},
					},
				},
			},
			want: true,
		},
		{
			name: "label removed",
			event: event.UpdateEvent{
				ObjectOld: &corev1.Namespace{
					ObjectMeta: metav1.ObjectMeta{
						Labels: map[string]string{
							"example.com/watch": "true",
						},
					},
				},
				ObjectNew: &corev1.Namespace{
					ObjectMeta: metav1.ObjectMeta{
						Labels: map[string]string{
							"other": "value",
						},
					},
				},
			},
			want: true,
		},
		{
			name: "label absent in both",
			event: event.UpdateEvent{
				ObjectOld: &corev1.Namespace{
					ObjectMeta: metav1.ObjectMeta{
						Labels: map[string]string{
							"other": "one",
						},
					},
				},
				ObjectNew: &corev1.Namespace{
					ObjectMeta: metav1.ObjectMeta{
						Labels: map[string]string{
							"other": "two",
						},
					},
				},
			},
			want: false,
		},
		{
			name: "nil labels in both",
			event: event.UpdateEvent{
				ObjectOld: &corev1.Namespace{},
				ObjectNew: &corev1.Namespace{},
			},
			want: false,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := p.Update(tt.event)
			if got != tt.want {
				t.Fatalf("Update() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestLabelPresentPredicate_Generic(t *testing.T) {
	t.Parallel()

	p := predicates.LabelPresentPredicate{Label: "example.com/watch"}

	if got := p.Generic(event.GenericEvent{}); got {
		t.Fatalf("Generic() = %v, want false", got)
	}
}
