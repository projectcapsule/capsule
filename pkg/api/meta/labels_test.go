// Copyright 2020-2025 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package meta_test

import (
	"reflect"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/projectcapsule/capsule/pkg/api/meta"
)

func TestFreezeLabel(t *testing.T) {
	ns := &corev1.Namespace{}
	ns.SetLabels(map[string]string{})

	// absent
	if meta.FreezeLabelTriggers(ns) {
		t.Errorf("expected FreezeLabelTriggers to be false when label is absent")
	}

	// set to trigger
	ns.Labels[meta.FreezeLabel] = meta.FreezeLabelTrigger
	if !meta.FreezeLabelTriggers(ns) {
		t.Errorf("expected FreezeLabelTriggers to be true when label is set to trigger")
	}

	ns.Labels[meta.FreezeLabel] = "false"
	if meta.FreezeLabelTriggers(ns) {
		t.Errorf("expected FreezeLabelTriggers to be false when label is not set to trigger")
	}

	// remove
	meta.FreezeLabelRemove(ns)
	if _, ok := ns.Labels[meta.FreezeLabel]; ok {
		t.Errorf("expected FreezeLabel to be removed")
	}
}

func TestOwnerPromotionLabel(t *testing.T) {
	ns := &corev1.Namespace{}
	ns.SetLabels(map[string]string{})

	if meta.OwnerPromotionLabelTriggers(ns) {
		t.Errorf("expected OwnerPromotionLabelTriggers to be false when label is absent")
	}

	ns.Labels[meta.OwnerPromotionLabel] = meta.OwnerPromotionLabelTrigger
	if !meta.OwnerPromotionLabelTriggers(ns) {
		t.Errorf("expected OwnerPromotionLabelTriggers to be true when label is set to trigger")
	}

	ns.Labels[meta.OwnerPromotionLabel] = "false"
	if meta.OwnerPromotionLabelTriggers(ns) {
		t.Errorf("expected OwnerPromotionLabelTriggers to be false when label is not set to trigger")
	}

	meta.OwnerPromotionLabelRemove(ns)
	if _, ok := ns.Labels[meta.OwnerPromotionLabel]; ok {
		t.Errorf("expected OwnerPromotionLabel to be removed")
	}
}

func TestSetFilteredLabels(t *testing.T) {
	type testCase struct {
		name   string
		obj    *unstructured.Unstructured
		filter map[string]struct{}
		want   map[string]string
	}

	newObjWithLabels := func(labels map[string]string) *unstructured.Unstructured {
		u := &unstructured.Unstructured{}
		u.SetLabels(labels)
		return u
	}

	tests := []testCase{
		{
			name:   "nil obj - no panic",
			obj:    nil,
			filter: map[string]struct{}{"a": {}},
			want:   nil,
		},
		{
			name:   "empty filter - object unchanged",
			obj:    newObjWithLabels(map[string]string{"a": "1", "b": "2"}),
			filter: map[string]struct{}{},
			want:   map[string]string{"a": "1", "b": "2"},
		},
		{
			name:   "nil labels - stays nil (no-op removal)",
			obj:    newObjWithLabels(nil),
			filter: map[string]struct{}{"a": {}},
			want:   nil,
		},
		{
			name:   "removes single reserved label",
			obj:    newObjWithLabels(map[string]string{"keep": "x", "rm": "y"}),
			filter: map[string]struct{}{"rm": {}},
			want:   map[string]string{"keep": "x"},
		},
		{
			name:   "removes multiple reserved labels",
			obj:    newObjWithLabels(map[string]string{"a": "1", "b": "2", "c": "3"}),
			filter: map[string]struct{}{"a": {}, "c": {}},
			want:   map[string]string{"b": "2"},
		},
		{
			name:   "filter contains keys not present - unchanged",
			obj:    newObjWithLabels(map[string]string{"a": "1"}),
			filter: map[string]struct{}{"missing": {}},
			want:   map[string]string{"a": "1"},
		},
		{
			name:   "removes all labels -> labels becomes empty map or nil (accept either)",
			obj:    newObjWithLabels(map[string]string{"a": "1"}),
			filter: map[string]struct{}{"a": {}},
			want:   map[string]string{},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			meta.SetFilteredLabels(tc.obj, tc.filter)

			if tc.obj == nil {
				return
			}

			got := tc.obj.GetLabels()

			if tc.want != nil && len(tc.want) == 0 {
				if got == nil || len(got) == 0 {
					return
				}
				t.Fatalf("expected labels to be empty or nil, got: %#v", got)
			}

			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("labels mismatch\nwant: %#v\ngot:  %#v", tc.want, got)
			}
		})
	}
}

func TestSetFilteredLabels_DoesNotMutateFilter(t *testing.T) {
	u := &unstructured.Unstructured{}
	u.SetLabels(map[string]string{"a": "1", "b": "2"})

	filter := map[string]struct{}{"a": {}}
	filterBefore := copyStructSet(filter)

	meta.SetFilteredLabels(u, filter)

	if !reflect.DeepEqual(filter, filterBefore) {
		t.Fatalf("filter map was mutated\nbefore: %#v\nafter:  %#v", filterBefore, filter)
	}
}

func copyStructSet(in map[string]struct{}) map[string]struct{} {
	if in == nil {
		return nil
	}
	out := make(map[string]struct{}, len(in))
	for k := range in {
		out[k] = struct{}{}
	}
	return out
}
