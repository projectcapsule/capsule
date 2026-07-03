// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package watch

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	toolscache "k8s.io/client-go/tools/cache"
)

func TestTransformStripHeavyMetadata(t *testing.T) {
	obj := &metav1.PartialObjectMetadata{ObjectMeta: metav1.ObjectMeta{
		Name:      "s1",
		Namespace: "ns",
		Labels:    map[string]string{"app": "x"},
		Annotations: map[string]string{
			corev1.LastAppliedConfigAnnotation: `{"apiVersion":"v1","kind":"Secret"}`,
			"keep.me":                          "yes",
		},
		ManagedFields: []metav1.ManagedFieldsEntry{{Manager: "kubectl"}},
	}}

	out, err := TransformStripHeavyMetadata(obj)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got, ok := out.(*metav1.PartialObjectMetadata)
	if !ok {
		t.Fatalf("expected the same object back, got %T", out)
	}

	if got.ManagedFields != nil {
		t.Fatalf("managedFields must be stripped, got %v", got.ManagedFields)
	}

	if _, ok := got.Annotations[corev1.LastAppliedConfigAnnotation]; ok {
		t.Fatalf("last-applied annotation must be stripped")
	}

	// Fields sinks match on survive.
	if got.Annotations["keep.me"] != "yes" || got.Labels["app"] != "x" {
		t.Fatalf("unrelated annotations and labels must survive, got %v / %v", got.Annotations, got.Labels)
	}

	// Annotations reduced to only the stripped key collapse to nil.
	onlyStripped := &metav1.PartialObjectMetadata{ObjectMeta: metav1.ObjectMeta{
		Name:        "s2",
		Annotations: map[string]string{corev1.LastAppliedConfigAnnotation: "{}"},
	}}

	out, err = TransformStripHeavyMetadata(onlyStripped)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if ann := out.(*metav1.PartialObjectMetadata).Annotations; ann != nil {
		t.Fatalf("annotations should collapse to nil, got %v", ann)
	}

	// Tombstones are unwrapped and their inner object stripped.
	inner := &metav1.PartialObjectMetadata{ObjectMeta: metav1.ObjectMeta{
		Name:          "s3",
		ManagedFields: []metav1.ManagedFieldsEntry{{Manager: "kubectl"}},
	}}
	tombstone := toolscache.DeletedFinalStateUnknown{Key: "ns/s3", Obj: inner}

	out, err = TransformStripHeavyMetadata(tombstone)
	if err != nil || out != any(tombstone) {
		t.Fatalf("tombstones must pass through as-is, got %v / %v", out, err)
	}

	if inner.ManagedFields != nil {
		t.Fatalf("tombstone's inner object must be stripped, got %v", inner.ManagedFields)
	}

	// Non-objects pass through untouched.
	notAnObject := struct{ any }{}
	if out, err := TransformStripHeavyMetadata(notAnObject); err != nil || out != notAnObject {
		t.Fatalf("non-objects must pass through, got %v / %v", out, err)
	}
}
