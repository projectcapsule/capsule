// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package meta_test

import (
	"context"
	"reflect"
	"testing"

	"github.com/projectcapsule/capsule/pkg/api/meta"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestReleaseAndReconcileAnnotations(t *testing.T) {
	t.Parallel()

	obj := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{
		meta.ReleaseAnnotation:   "TRUE",
		meta.ReconcileAnnotation: "now",
		"keep":                   "value",
	}}}

	if !meta.ReleaseAnnotationTriggers(obj) {
		t.Fatalf("ReleaseAnnotationTriggers() = false, want true")
	}
	meta.ReleaseAnnotationRemove(obj)
	if _, ok := obj.Annotations[meta.ReleaseAnnotation]; ok {
		t.Fatalf("ReleaseAnnotationRemove() did not remove annotation")
	}

	meta.RemoveReconcileTriggerAnnotation(obj)
	if _, ok := obj.Annotations[meta.ReconcileAnnotation]; ok {
		t.Fatalf("RemoveReconcileTriggerAnnotation() did not remove reconcile annotation")
	}
	if obj.Annotations["keep"] != "value" {
		t.Fatalf("RemoveReconcileTriggerAnnotation() removed unrelated annotation")
	}

	onlyReconcile := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{meta.ReconcileAnnotation: "now"}}}
	meta.RemoveReconcileTriggerAnnotation(onlyReconcile)
	if onlyReconcile.Annotations != nil {
		t.Fatalf("RemoveReconcileTriggerAnnotation() = %#v, want nil when last annotation removed", onlyReconcile.Annotations)
	}
}

func TestTriggerRequestReconcileAnnotation(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	gvk := schema.GroupVersionKind{Group: "", Version: "v1", Kind: "ConfigMap"}
	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(gvk)
	obj.SetName("settings")
	obj.SetNamespace("default")

	cl := fake.NewClientBuilder().WithObjects(obj).Build()
	if err := meta.TriggerRequestReconcileAnnotation(ctx, cl, gvk, types.NamespacedName{Namespace: "default", Name: "settings"}); err != nil {
		t.Fatalf("TriggerRequestReconcileAnnotation() unexpected error: %v", err)
	}

	got := &unstructured.Unstructured{}
	got.SetGroupVersionKind(gvk)
	if err := cl.Get(ctx, client.ObjectKey{Namespace: "default", Name: "settings"}, got); err != nil {
		t.Fatalf("getting patched object: %v", err)
	}
	if got.GetAnnotations()[meta.ReconcileAnnotation] == "" {
		t.Fatalf("TriggerRequestReconcileAnnotation() did not set annotation")
	}
}

func TestConditionHelpers(t *testing.T) {
	t.Parallel()

	obj := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Generation: 7}}

	conditions := meta.ConditionList{meta.NewReadyCondition(obj)}
	if !meta.IsStatusConditionTrue(conditions, meta.ReadyCondition) {
		t.Fatalf("IsStatusConditionTrue() = false, want true")
	}
	if meta.IsStatusConditionTrue(conditions, meta.CordonedCondition) {
		t.Fatalf("IsStatusConditionTrue() = true for missing condition")
	}

	var list meta.ConditionList
	if updated := list.UpdateConditionByTypeWithStatus(meta.NewCordonedCondition(obj)); !updated {
		t.Fatalf("UpdateConditionByTypeWithStatus() = false for appended condition")
	}
	if updated := list.UpdateConditionByTypeWithStatus(meta.NewCordonedCondition(obj)); updated {
		t.Fatalf("UpdateConditionByTypeWithStatus() = true for identical condition")
	}

	tests := []struct {
		name string
		cond meta.Condition
		typ  string
	}{
		{name: "cordoned", cond: meta.NewCordonedCondition(obj), typ: meta.CordonedCondition},
		{name: "exhausted", cond: meta.NewExhaustedCondition(obj), typ: meta.ExhaustedCondition},
		{name: "bound", cond: meta.NewBoundCondition(obj), typ: meta.BoundCondition},
		{name: "assigned", cond: meta.NewAssignedCondition(obj), typ: meta.AssignedCondition},
		{name: "ready reconciling", cond: meta.NewReadyConditionReconcilingReason(obj), typ: meta.ReadyCondition},
		{name: "terminating", cond: meta.NewTerminatingConditionReason(obj), typ: meta.TerminatingCondition},
	}

	for _, tt := range tests {
		if tt.cond.Type != tt.typ {
			t.Fatalf("%s condition type = %q, want %q", tt.name, tt.cond.Type, tt.typ)
		}
		if tt.cond.Type != meta.CordonedCondition && tt.cond.ObservedGeneration != 0 && tt.cond.ObservedGeneration != obj.Generation {
			t.Fatalf("%s observed generation = %d", tt.name, tt.cond.ObservedGeneration)
		}
	}
}

func TestManagerAndNameHelpers(t *testing.T) {
	t.Parallel()

	if got := meta.ControllerFieldOwnerPrefix("webhook"); got != "projectcapsule.dev/webhook" {
		t.Fatalf("ControllerFieldOwnerPrefix() = %q", got)
	}
	if got := meta.ControllerFieldOwner(); got != meta.FieldManagerCapsuleController {
		t.Fatalf("ControllerFieldOwner() = %q", got)
	}
	if got := meta.ResourceControllerFieldOwnerPrefix(); got != "projectcapsule.dev/resource/controller" {
		t.Fatalf("ResourceControllerFieldOwnerPrefix() = %q", got)
	}
	if got := meta.NameForManagedRuleStatus(); got != "capsule-managed-rules" {
		t.Fatalf("NameForManagedRuleStatus() = %q", got)
	}
	if got := meta.NameForManagedRoleBindings("hash"); got != "capsule:managed:hash" {
		t.Fatalf("NameForManagedRoleBindings() = %q", got)
	}
	if got := meta.NameForManagedPoolResourceQuota("pool-a"); got != "capsule-pool-pool-a" {
		t.Fatalf("NameForManagedPoolResourceQuota() = %q", got)
	}
}

func TestCapsuleFieldOwners(t *testing.T) {
	t.Parallel()

	obj := &unstructured.Unstructured{}
	obj.SetManagedFields([]metav1.ManagedFieldsEntry{
		{Manager: "projectcapsule.dev/controller"},
		{Manager: "projectcapsule.dev/resource/controller"},
		{Manager: "kubectl"},
		{Manager: ""},
	})

	got := meta.CapsuleFieldOwners(obj, meta.FieldManagerCapsulePrefix)
	want := map[string]struct{}{
		"projectcapsule.dev/controller":          {},
		"projectcapsule.dev/resource/controller": {},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("CapsuleFieldOwners() = %#v, want %#v", got, want)
	}
	if !meta.HasExactlyCapsuleOwners(obj, meta.FieldManagerCapsulePrefix, []string{
		"projectcapsule.dev/controller",
		"projectcapsule.dev/resource/controller",
	}) {
		t.Fatalf("HasExactlyCapsuleOwners() = false, want true")
	}
	if meta.HasExactlyCapsuleOwners(obj, meta.FieldManagerCapsulePrefix, []string{"projectcapsule.dev/controller"}) {
		t.Fatalf("HasExactlyCapsuleOwners() = true, want false for missing allowed owner")
	}
	if owners := meta.CapsuleFieldOwners(nil, meta.FieldManagerCapsulePrefix); len(owners) != 0 {
		t.Fatalf("CapsuleFieldOwners(nil) = %#v, want empty", owners)
	}
}

func TestReferenceStrings(t *testing.T) {
	t.Parallel()

	if got := meta.RFC1123Name("name").String(); got != "name" {
		t.Fatalf("RFC1123Name.String() = %q", got)
	}
	if got := meta.RFC1123SubdomainName("namespace").String(); got != "namespace" {
		t.Fatalf("RFC1123SubdomainName.String() = %q", got)
	}
}

func TestLabelsChangedUnstructuredAndSelectorKeys(t *testing.T) {
	t.Parallel()

	oldObj := &unstructured.Unstructured{}
	oldObj.SetLabels(map[string]string{"app": "api"})
	newObj := oldObj.DeepCopy()
	newObj.SetLabels(map[string]string{"app": "worker"})

	if !meta.LabelsChangedUnstructured(*oldObj, *newObj) {
		t.Fatalf("LabelsChangedUnstructured() = false, want true")
	}

	selector := &metav1.LabelSelector{
		MatchLabels: map[string]string{"app": "api"},
		MatchExpressions: []metav1.LabelSelectorRequirement{{
			Key:      "tier",
			Operator: metav1.LabelSelectorOpIn,
			Values:   []string{"backend"},
		}},
	}
	if got := meta.LabelSelectorKeys(selector); !reflect.DeepEqual(got, map[string]struct{}{"app": {}, "tier": {}}) {
		t.Fatalf("LabelSelectorKeys() = %#v, want app and tier", got)
	}
	if got := meta.LabelSelectorKeys(nil); len(got) != 0 {
		t.Fatalf("LabelSelectorKeys(nil) = %#v, want empty", got)
	}
}

func TestHelpersSchemeImport(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatalf("adding core scheme: %v", err)
	}
}
