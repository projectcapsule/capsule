// Copyright 2020-2025 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package predicates_test

import (
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/event"

	"github.com/projectcapsule/capsule/pkg/runtime/predicates"
)

func TestUpdatedMetadataPredicate_StaticFuncs(t *testing.T) {
	t.Parallel()

	p := predicates.UpdatedLabelsPredicate{}

	if got := p.Generic(event.GenericEvent{}); got {
		t.Fatalf("Generic() = %v, want false", got)
	}
	if got := p.Create(event.CreateEvent{}); !got {
		t.Fatalf("Create() = %v, want true", got)
	}
	if got := p.Delete(event.DeleteEvent{}); !got {
		t.Fatalf("Delete() = %v, want true", got)
	}
}

func TestUpdatedMetadataPredicate_Update(t *testing.T) {
	t.Parallel()

	p := predicates.UpdatedLabelsPredicate{}

	mk := func(lbl map[string]string) *unstructured.Unstructured {
		u := &unstructured.Unstructured{}
		u.SetAPIVersion("v1")
		u.SetKind("ConfigMap")
		u.SetName("cm")
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
		{"both nil", nil, nil, false},
		{"nil to empty", nil, map[string]string{}, false},
		{"same labels", map[string]string{"a": "1"}, map[string]string{"a": "1"}, false},
		{"label added", nil, map[string]string{"a": "1"}, true},
		{"label removed", map[string]string{"a": "1"}, nil, true},
		{"label value changed", map[string]string{"a": "1"}, map[string]string{"a": "2"}, true},
		{"label key changed", map[string]string{"a": "1"}, map[string]string{"b": "1"}, true},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			ev := event.UpdateEvent{ObjectOld: mk(tt.old), ObjectNew: mk(tt.new)}
			if got := p.Update(ev); got != tt.want {
				t.Fatalf("Update() = %v, want %v", got, tt.want)
			}
		})
	}
}
