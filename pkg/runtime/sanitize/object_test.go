// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package sanitize_test

import (
	"testing"

	apiMeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/projectcapsule/capsule/pkg/runtime/sanitize"
)

func TestSanitizeObject_Nil(t *testing.T) {
	t.Parallel()

	// Should not panic and should return nil.
	if err := sanitize.SanitizeObject(nil, nil, sanitize.SanitizeOptions{
		StripUID:           true,
		StripManagedFields: true,
		StripLastApplied:   true,
		StripStatus:        true,
	}); err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
}

func TestSanitizeObject_MetadataFields_TypedObject(t *testing.T) {
	t.Parallel()

	pod := newPodWithMeta()

	opts := sanitize.SanitizeOptions{
		StripUID:           true,
		StripManagedFields: true,
		StripLastApplied:   true,
		StripStatus:        false, // metadata-only test
	}

	if err := sanitize.SanitizeObject(pod, nil, opts); err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}

	// UID stripped
	if got := pod.GetUID(); got != "" {
		t.Fatalf("expected UID stripped, got %q", got)
	}

	// ManagedFields stripped
	accessor, err := apiMeta.Accessor(pod)
	if err != nil {
		t.Fatalf("apiMeta.Accessor failed: %v", err)
	}
	if mf := accessor.GetManagedFields(); len(mf) != 0 {
		t.Fatalf("expected managedFields stripped, got %#v", mf)
	}

	// last-applied stripped, other annotation preserved
	anns := pod.GetAnnotations()
	if _, ok := anns["kubectl.kubernetes.io/last-applied-configuration"]; ok {
		t.Fatalf("expected last-applied annotation stripped, still present: %#v", anns)
	}
	if anns["keep"] != "yes" {
		t.Fatalf("expected other annotation preserved, got %#v", anns)
	}
}

func TestSanitizeObject_LastApplied_AnnotationMapRemovedWhenEmpty(t *testing.T) {
	t.Parallel()

	pod := newPodWithMeta()
	// Only last-applied exists.
	pod.SetAnnotations(map[string]string{
		"kubectl.kubernetes.io/last-applied-configuration": `{"x":"y"}`,
	})

	opts := sanitize.SanitizeOptions{
		StripLastApplied: true,
	}

	if err := sanitize.SanitizeObject(pod, nil, opts); err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}

	if anns := pod.GetAnnotations(); len(anns) != 0 {
		t.Fatalf("expected annotations cleared (nil or empty) after removing last-applied, got %#v", anns)
	}
}

func TestSanitizeObject_NoOptions_NoChanges(t *testing.T) {
	t.Parallel()

	pod := newPodWithMeta()
	// make a copy for comparison
	orig := pod.DeepCopy()

	opts := sanitize.SanitizeOptions{} // everything false

	if err := sanitize.SanitizeObject(pod, nil, opts); err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}

	// Verify important bits unchanged
	if pod.GetUID() != orig.GetUID() {
		t.Fatalf("UID changed unexpectedly: %q -> %q", orig.GetUID(), pod.GetUID())
	}
	if pod.GetAnnotations()["keep"] != "yes" {
		t.Fatalf("annotations changed unexpectedly: %#v", pod.GetAnnotations())
	}

	// ManagedFields should still be present
	accessor, err := apiMeta.Accessor(pod)
	if err != nil {
		t.Fatalf("apiMeta.Accessor failed: %v", err)
	}
	origAcc, _ := apiMeta.Accessor(orig)
	if len(accessor.GetManagedFields()) != len(origAcc.GetManagedFields()) {
		t.Fatalf("managedFields changed unexpectedly: %#v -> %#v", origAcc.GetManagedFields(), accessor.GetManagedFields())
	}
}

func TestSanitizeObject_StripStatus_TypedObject(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme: %v", err)
	}

	pod := newPodWithMeta()
	// Put something into status so we can confirm it’s removed.
	pod.Status.Phase = corev1.PodRunning
	pod.Status.HostIP = "10.0.0.1"

	opts := sanitize.SanitizeOptions{
		StripStatus: true,
	}

	if err := sanitize.SanitizeObject(pod, scheme, opts); err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}

	// After stripping status, it should be zero value.
	if pod.Status.Phase != "" || pod.Status.HostIP != "" {
		t.Fatalf("expected pod status stripped to zero value, got %#v", pod.Status)
	}
}

func TestSanitizeObject_StripStatus_RequiresScheme(t *testing.T) {
	t.Parallel()

	pod := newPodWithMeta()
	pod.Status.Phase = corev1.PodRunning

	opts := sanitize.SanitizeOptions{
		StripStatus: true,
	}

	if err := sanitize.SanitizeObject(pod, nil, opts); err == nil {
		t.Fatalf("expected error when StripStatus=true and scheme=nil, got nil")
	}
}

func TestSanitizeObject_Unstructured_FastPathMetadataAndStatus(t *testing.T) {
	t.Parallel()

	u := &unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "v1",
			"kind":       "Pod",
			"metadata": map[string]any{
				"name":      "p",
				"namespace": "ns",
				"uid":       "abc",
				"annotations": map[string]any{
					"kubectl.kubernetes.io/last-applied-configuration": `{"x":"y"}`,
					"keep": "yes",
				},
				"managedFields": []any{
					map[string]any{"manager": "x"},
				},
			},
			"status": map[string]any{
				"phase": "Running",
			},
		},
	}

	// If your SanitizeObject supports unstructured directly (recommended),
	// this should work. If you keep a separate SanitizeUnstructured, then
	// call that instead in this test.
	opts := sanitize.SanitizeOptions{
		StripUID:           true,
		StripManagedFields: true,
		StripLastApplied:   true,
		StripStatus:        true,
	}

	// scheme not needed for unstructured if your implementation detects it and uses map operations.
	// If your implementation requires scheme even for unstructured, pass a scheme.
	if err := sanitize.SanitizeObject(u, runtime.NewScheme(), opts); err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}

	// Verify uid removed
	if _, found, _ := unstructured.NestedFieldNoCopy(u.Object, "metadata", "uid"); found {
		t.Fatalf("expected metadata.uid stripped")
	}
	// Verify managedFields removed
	if _, found, _ := unstructured.NestedFieldNoCopy(u.Object, "metadata", "managedFields"); found {
		t.Fatalf("expected metadata.managedFields stripped")
	}
	// Verify last-applied removed, keep preserved
	anns, found, err := unstructured.NestedStringMap(u.Object, "metadata", "annotations")
	if err != nil || !found {
		t.Fatalf("expected annotations map to exist, err=%v found=%v", err, found)
	}
	if _, ok := anns["kubectl.kubernetes.io/last-applied-configuration"]; ok {
		t.Fatalf("expected last-applied stripped from annotations, got %#v", anns)
	}
	if anns["keep"] != "yes" {
		t.Fatalf("expected keep annotation preserved, got %#v", anns)
	}
	// Verify status removed
	if _, found, _ := unstructured.NestedFieldNoCopy(u.Object, "status"); found {
		t.Fatalf("expected status stripped")
	}
}

func TestSanitizeObject_AllOptions_TypedObject(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme: %v", err)
	}

	pod := newPodWithMeta()
	pod.Status.Phase = corev1.PodRunning
	pod.Status.PodIP = "10.0.0.2"

	opts := sanitize.SanitizeOptions{
		StripUID:           true,
		StripManagedFields: true,
		StripLastApplied:   true,
		StripStatus:        true,
	}

	if err := sanitize.SanitizeObject(pod, scheme, opts); err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}

	// UID stripped
	if pod.GetUID() != "" {
		t.Fatalf("expected UID stripped, got %q", pod.GetUID())
	}

	// managedFields stripped
	acc, err := apiMeta.Accessor(pod)
	if err != nil {
		t.Fatalf("apiMeta.Accessor failed: %v", err)
	}
	if len(acc.GetManagedFields()) != 0 {
		t.Fatalf("expected managedFields stripped, got %#v", acc.GetManagedFields())
	}

	// last-applied stripped
	anns := pod.GetAnnotations()
	if _, ok := anns["kubectl.kubernetes.io/last-applied-configuration"]; ok {
		t.Fatalf("expected last-applied stripped, got %#v", anns)
	}
	if anns["keep"] != "yes" {
		t.Fatalf("expected other annotations preserved, got %#v", anns)
	}

	if pod.Status.Phase != "" || pod.Status.PodIP != "" || pod.Status.HostIP != "" {
		t.Fatalf("expected scalar status fields cleared, got %#v", pod.Status)
	}
	if len(pod.Status.Conditions) != 0 {
		t.Fatalf("expected status.conditions empty, got %#v", pod.Status.Conditions)
	}
	if len(pod.Status.ContainerStatuses) != 0 {
		t.Fatalf("expected status.containerStatuses empty, got %#v", pod.Status.ContainerStatuses)
	}
}

// --- helpers ---

func newPodWithMeta() *corev1.Pod {
	p := &corev1.Pod{}
	p.SetName("p")
	p.SetNamespace("ns")
	p.SetUID(types.UID("uid-123"))
	p.SetAnnotations(map[string]string{
		"kubectl.kubernetes.io/last-applied-configuration": `{"a":"b"}`,
		"keep": "yes",
	})

	// ManagedFields is on ObjectMeta; easiest to set via Accessor.
	// Note: type is []metav1.ManagedFieldsEntry
	acc, _ := apiMeta.Accessor(p)
	acc.SetManagedFields([]metav1.ManagedFieldsEntry{
		{
			Manager:    "test",
			Operation:  metav1.ManagedFieldsOperationApply,
			APIVersion: "v1",
			Time:       &metav1.Time{},
		},
	})

	// Implement client.Object assertion at compile-time for sanity.
	var _ client.Object = p

	return p
}
