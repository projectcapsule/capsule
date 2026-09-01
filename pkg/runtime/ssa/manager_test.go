// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package ssa

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	k8smeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/projectcapsule/capsule/pkg/api/meta"
	clt "github.com/projectcapsule/capsule/pkg/runtime/client"
)

const (
	testCreatedBy  = "test-controller"
	testFieldOwner = "projectcapsule.dev/resource/test/request"
)

func TestManagedMetadataPatches(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}

	manager := Manager{Metadata: Metadata{
		CreatedByValue:                      testCreatedBy,
		ManagedByValue:                      testCreatedBy,
		ProtectedByValue:                    testCreatedBy,
		ProtectedByServiceAccount:           "system:serviceaccount:test:runner",
		ProtectedByServiceAccountAnnotation: meta.BreakRequestServiceAccountAnnotation,
	}}
	owner := metav1.OwnerReference{
		APIVersion: "capsule.clastix.io/v1beta2",
		Kind:       "BreakRequest",
		Name:       "request",
		UID:        types.UID("request-uid"),
	}

	t.Run("new resource is marked as created and owned", func(t *testing.T) {
		c := fake.NewClientBuilder().WithScheme(scheme).Build()
		obj := configMap("new", nil)

		patches, created, err := manager.managedMetadataPatches(
			context.Background(),
			c,
			obj,
			ApplyOptions{OwnerReference: &owner},
		)
		if err != nil {
			t.Fatalf("managedMetadataPatches() error = %v", err)
		}
		if !created {
			t.Fatal("managedMetadataPatches() created = false, want true")
		}

		assertPatchValue(t, patches, "/metadata/labels/projectcapsule.dev~1created-by", testCreatedBy)
		assertPatchValue(t, patches, "/metadata/labels/projectcapsule.dev~1managed-by", testCreatedBy)
		assertPatchValue(t, patches, "/metadata/ownerReferences/-", &owner)
	})

	t.Run("existing resource requires adoption and is not owned", func(t *testing.T) {
		existing := configMap("existing", map[string]any{"existing": "value"})
		c := fake.NewClientBuilder().WithScheme(scheme).WithRuntimeObjects(existing).WithReturnManagedFields().Build()
		desired := configMap("existing", map[string]any{"requested": "value"})

		if _, _, err := manager.managedMetadataPatches(
			context.Background(),
			c,
			desired,
			ApplyOptions{},
		); err == nil {
			t.Fatal("managedMetadataPatches() error = nil, want adoption error")
		}

		patches, created, err := manager.managedMetadataPatches(
			context.Background(),
			c,
			desired,
			ApplyOptions{Adopt: true, OwnerReference: &owner},
		)
		if err != nil {
			t.Fatalf("managedMetadataPatches() with adoption error = %v", err)
		}
		if created {
			t.Fatal("managedMetadataPatches() created = true, want false")
		}

		assertPatchValue(t, patches, "/metadata/labels/projectcapsule.dev~1managed-by", testCreatedBy)
		assertNoPatchPath(t, patches, "/metadata/labels/projectcapsule.dev~1created-by")
		assertNoPatchPath(t, patches, "/metadata/ownerReferences/-")
	})

	t.Run("an interrupted creation is recovered from managed fields", func(t *testing.T) {
		existing := configMap("interrupted", map[string]any{"requested": "value"})
		existing.SetManagedFields([]metav1.ManagedFieldsEntry{managedField(testFieldOwner)})
		c := fake.NewClientBuilder().WithScheme(scheme).WithRuntimeObjects(existing).WithReturnManagedFields().Build()

		patches, created, err := manager.managedMetadataPatches(
			context.Background(),
			c,
			existing,
			ApplyOptions{FieldOwner: testFieldOwner, OwnerReference: &owner},
		)
		if err != nil {
			t.Fatalf("managedMetadataPatches() recovery error = %v", err)
		}
		if !created {
			t.Fatal("managedMetadataPatches() created = false, want recovered creation")
		}

		assertPatchValue(t, patches, "/metadata/labels/projectcapsule.dev~1created-by", testCreatedBy)
		assertPatchValue(t, patches, "/metadata/ownerReferences/-", &owner)
	})
}

func TestApplyUsesServerSideApply(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}

	base := fake.NewClientBuilder().WithScheme(scheme).Build()
	recording := &applyingClient{Client: base}
	manager := Manager{Metadata: Metadata{
		CreatedByValue:                      testCreatedBy,
		ManagedByValue:                      testCreatedBy,
		ProtectedByValue:                    testCreatedBy,
		ProtectedByServiceAccount:           "system:serviceaccount:test:runner",
		ProtectedByServiceAccountAnnotation: meta.BreakRequestServiceAccountAnnotation,
	}}
	desired := configMap("applied", map[string]any{"requested": "value"})
	desired.SetLabels(map[string]string{
		meta.CreatedByCapsuleLabel:    "template-value",
		meta.NewManagedByCapsuleLabel: "template-value",
		meta.ProtectedByCapsuleLabel:  "template-value",
	})
	desired.SetAnnotations(map[string]string{
		meta.BreakRequestServiceAccountAnnotation: "system:serviceaccount:spoofed:runner",
	})

	result, err := manager.Apply(context.Background(), recording, desired, ApplyOptions{
		FieldOwner: testFieldOwner,
		Force:      true,
		Protect:    true,
	})
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	if !result.Created {
		t.Fatal("Apply() created = false, want true")
	}
	if len(recording.patches) != 2 {
		t.Fatalf("Apply() patches = %d, want SSA and metadata patches", len(recording.patches))
	}

	apply := recording.patches[0]
	if apply.patchType != types.ApplyPatchType {
		t.Fatalf("Apply() patch type = %q, want %q", apply.patchType, types.ApplyPatchType)
	}
	if apply.options.FieldManager != testFieldOwner {
		t.Fatalf("Apply() field manager = %q, want %q", apply.options.FieldManager, testFieldOwner)
	}
	if apply.options.Force == nil || !*apply.options.Force {
		t.Fatal("Apply() force ownership was not enabled")
	}
	if data, found, dataErr := unstructured.NestedStringMap(apply.object.Object, "data"); dataErr != nil || !found || data["requested"] != "value" {
		t.Fatalf("Apply() data = %#v, found=%v, error=%v", data, found, dataErr)
	}
	if labels := apply.object.GetLabels(); labels[meta.CreatedByCapsuleLabel] != "" || labels[meta.NewManagedByCapsuleLabel] != "" {
		t.Fatalf("Apply() allowed rendered tracking labels: %#v", labels)
	}
	if value := apply.object.GetLabels()[meta.ProtectedByCapsuleLabel]; value != testCreatedBy {
		t.Fatalf("Apply() protection label = %q, want %q", value, testCreatedBy)
	}
	if value := apply.object.GetAnnotations()[meta.BreakRequestServiceAccountAnnotation]; value != "system:serviceaccount:test:runner" {
		t.Fatalf("Apply() protection ServiceAccount = %q, want resolved identity", value)
	}
}

func TestApplyStripsProtectionWhenDisabled(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}

	base := fake.NewClientBuilder().WithScheme(scheme).Build()
	recording := &applyingClient{Client: base}
	manager := Manager{Metadata: Metadata{
		CreatedByValue:                      testCreatedBy,
		ManagedByValue:                      testCreatedBy,
		ProtectedByValue:                    testCreatedBy,
		ProtectedByServiceAccount:           "system:serviceaccount:test:runner",
		ProtectedByServiceAccountAnnotation: meta.BreakRequestServiceAccountAnnotation,
	}}
	desired := configMap("unprotected", nil)
	desired.SetLabels(map[string]string{meta.ProtectedByCapsuleLabel: "template-value"})
	desired.SetAnnotations(map[string]string{
		meta.BreakRequestServiceAccountAnnotation: "system:serviceaccount:spoofed:runner",
	})

	if _, err := manager.Apply(context.Background(), recording, desired, ApplyOptions{
		FieldOwner: testFieldOwner,
	}); err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	if value := recording.patches[0].object.GetLabels()[meta.ProtectedByCapsuleLabel]; value != "" {
		t.Fatalf("Apply() protection label = %q with protection disabled", value)
	}
	if value := recording.patches[0].object.GetAnnotations()[meta.BreakRequestServiceAccountAnnotation]; value != "" {
		t.Fatalf("Apply() protection ServiceAccount = %q with protection disabled", value)
	}
}

func TestApplyNormalizesClusterScope(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}

	mapper := k8smeta.NewDefaultRESTMapper([]schema.GroupVersion{{Version: "v1"}})
	mapper.Add(corev1.SchemeGroupVersion.WithKind("ConfigMap"), k8smeta.RESTScopeRoot)

	base := fake.NewClientBuilder().WithScheme(scheme).Build()
	recording := &applyingClient{Client: base}
	manager := Manager{
		Mapper: mapper,
		Metadata: Metadata{
			CreatedByValue: testCreatedBy,
			ManagedByValue: testCreatedBy,
		},
	}
	desired := configMap("cluster-scoped", map[string]any{"requested": "value"})

	if _, err := manager.Apply(context.Background(), recording, desired, ApplyOptions{
		FieldOwner: testFieldOwner,
	}); err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	if got := recording.patches[0].object.GetNamespace(); got != "" {
		t.Fatalf("Apply() namespace = %q for cluster-scoped object, want empty", got)
	}
}

func TestResolveResourceID(t *testing.T) {
	t.Parallel()

	mapper := k8smeta.NewDefaultRESTMapper([]schema.GroupVersion{{Version: "v1"}})
	mapper.Add(corev1.SchemeGroupVersion.WithKind("ConfigMap"), k8smeta.RESTScopeNamespace)
	mapper.Add(corev1.SchemeGroupVersion.WithKind("Namespace"), k8smeta.RESTScopeRoot)
	manager := Manager{Mapper: mapper}

	t.Run("namespaced", func(t *testing.T) {
		obj := configMap("config", nil)

		id, clusterScoped, err := manager.ResolveResourceID(obj, "tenant-a", "template")
		if err != nil {
			t.Fatalf("ResolveResourceID() error = %v", err)
		}
		if clusterScoped {
			t.Fatal("ResolveResourceID() clusterScoped = true, want false")
		}
		if id.Namespace != "default" || id.Name != "config" || id.Tenant != "tenant-a" || id.Origin != "template" {
			t.Fatalf("ResolveResourceID() = %#v", id)
		}
	})

	t.Run("cluster scoped", func(t *testing.T) {
		obj := &unstructured.Unstructured{}
		obj.SetAPIVersion("v1")
		obj.SetKind("Namespace")
		obj.SetName("managed")
		obj.SetNamespace("request-namespace")

		id, clusterScoped, err := manager.ResolveResourceID(obj, "", "")
		if err != nil {
			t.Fatalf("ResolveResourceID() error = %v", err)
		}
		if !clusterScoped {
			t.Fatal("ResolveResourceID() clusterScoped = false, want true")
		}
		if id.Namespace != "" || id.Name != "managed" {
			t.Fatalf("ResolveResourceID() = %#v", id)
		}
	})
}

func TestPrune(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}

	mapper := k8smeta.NewDefaultRESTMapper([]schema.GroupVersion{{Version: "v1"}})
	mapper.Add(corev1.SchemeGroupVersion.WithKind("ConfigMap"), k8smeta.RESTScopeNamespace)

	owner := metav1.OwnerReference{
		APIVersion: "capsule.clastix.io/v1beta2",
		Kind:       "BreakRequest",
		Name:       "request",
		UID:        types.UID("request-uid"),
	}
	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "default"}}
	manager := Manager{
		Mapper: mapper,
		Metadata: Metadata{
			CreatedByValue: testCreatedBy,
			ManagedByValue: testCreatedBy,
		},
	}

	t.Run("adopted resource is reduced to an identity apply patch", func(t *testing.T) {
		existing := configMap("adopted", map[string]any{"existing": "value"})
		base := fake.NewClientBuilder().WithScheme(scheme).WithRuntimeObjects(ns.DeepCopy(), existing).Build()
		recording := &recordingClient{Client: base}
		manager.Reader = base

		deleted, err := manager.Prune(
			context.Background(),
			recording,
			existing,
			PruneOptions{FieldOwner: testFieldOwner},
		)
		if err != nil {
			t.Fatalf("Prune() error = %v", err)
		}
		if deleted {
			t.Fatal("Prune() deleted = true, want false")
		}
		if len(recording.patchObjects) != 1 {
			t.Fatalf("Prune() patches = %d, want 1", len(recording.patchObjects))
		}

		patched := recording.patchObjects[0].(*unstructured.Unstructured)
		if patched.GetName() != existing.GetName() || patched.GetNamespace() != existing.GetNamespace() {
			t.Fatalf("prune patch identity = %s/%s, want %s/%s",
				patched.GetNamespace(), patched.GetName(), existing.GetNamespace(), existing.GetName())
		}
		if _, found, err := unstructured.NestedMap(patched.Object, "data"); err != nil || found {
			t.Fatalf("prune patch contains data: found=%v err=%v", found, err)
		}
	})

	t.Run("created resource without an owner reference is deleted by field ownership", func(t *testing.T) {
		existing := configMap("created-without-owner", map[string]any{"requested": "value"})
		existing.SetLabels(map[string]string{meta.CreatedByCapsuleLabel: testCreatedBy})
		existing.SetManagedFields([]metav1.ManagedFieldsEntry{
			managedField(testFieldOwner),
			managedField(meta.ResourceControllerFieldOwnerPrefix()),
		})
		base := fake.NewClientBuilder().
			WithScheme(scheme).
			WithRuntimeObjects(ns.DeepCopy(), existing).
			WithReturnManagedFields().
			Build()
		recording := &recordingClient{Client: base}
		manager.Reader = base

		deleted, err := manager.Prune(
			context.Background(),
			recording,
			existing,
			PruneOptions{FieldOwner: testFieldOwner},
		)
		if err != nil {
			t.Fatalf("Prune() error = %v", err)
		}
		if !deleted {
			t.Fatal("Prune() deleted = false, want true")
		}
		if len(recording.deleteObjects) != 1 {
			t.Fatalf("Prune() deletes = %d, want 1", len(recording.deleteObjects))
		}
	})

	t.Run("resource created for the owner is deleted", func(t *testing.T) {
		existing := configMap("created", map[string]any{"requested": "value"})
		existing.SetLabels(map[string]string{meta.CreatedByCapsuleLabel: testCreatedBy})
		existing.SetOwnerReferences([]metav1.OwnerReference{owner})
		base := fake.NewClientBuilder().WithScheme(scheme).WithRuntimeObjects(ns.DeepCopy(), existing).Build()
		recording := &recordingClient{Client: base}
		manager.Reader = base

		deleted, err := manager.Prune(
			context.Background(),
			recording,
			existing,
			PruneOptions{FieldOwner: testFieldOwner, OwnerReference: &owner},
		)
		if err != nil {
			t.Fatalf("Prune() error = %v", err)
		}
		if !deleted {
			t.Fatal("Prune() deleted = false, want true")
		}
		if len(recording.deleteObjects) != 1 {
			t.Fatalf("Prune() deletes = %d, want 1", len(recording.deleteObjects))
		}
		if len(recording.patchObjects) != 0 {
			t.Fatalf("Prune() patches = %d, want 0", len(recording.patchObjects))
		}
	})

	t.Run("created resource shared by another Capsule manager is retained", func(t *testing.T) {
		existing := configMap("shared", map[string]any{"requested": "value"})
		existing.SetLabels(map[string]string{meta.CreatedByCapsuleLabel: testCreatedBy})
		existing.SetManagedFields([]metav1.ManagedFieldsEntry{
			managedField(testFieldOwner),
			managedField(meta.ResourceControllerFieldOwnerPrefix()),
			managedField("projectcapsule.dev/resource/test/another-request"),
		})

		if manager.isDeletable(existing, PruneOptions{FieldOwner: testFieldOwner}) {
			t.Fatal("isDeletable() = true for a resource shared by another Capsule manager")
		}
	})
}

func TestOrphan(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}

	owner := metav1.OwnerReference{
		APIVersion: "capsule.clastix.io/v1beta2",
		Kind:       "BreakRequest",
		Name:       "request",
		UID:        types.UID("request-uid"),
	}
	existing := configMap("orphaned", map[string]any{"requested": "value"})
	existing.SetOwnerReferences([]metav1.OwnerReference{owner})
	existing.SetLabels(map[string]string{
		meta.CreatedByCapsuleLabel:    testCreatedBy,
		meta.NewManagedByCapsuleLabel: testCreatedBy,
		meta.ProtectedByCapsuleLabel:  testCreatedBy,
		meta.AppManagedByLabel:        "test-app-manager",
		"example.com/keep":            "true",
	})
	existing.SetAnnotations(map[string]string{
		meta.BreakRequestServiceAccountAnnotation: "system:serviceaccount:stale:runner",
		"example.com/keep":                        "true",
	})

	c := fake.NewClientBuilder().
		WithScheme(scheme).
		WithRuntimeObjects(existing).
		Build()
	manager := Manager{Metadata: Metadata{
		CreatedByValue:                      testCreatedBy,
		ManagedByValue:                      testCreatedBy,
		ProtectedByValue:                    testCreatedBy,
		ProtectedByServiceAccount:           "system:serviceaccount:test:runner",
		ProtectedByServiceAccountAnnotation: meta.BreakRequestServiceAccountAnnotation,
		AppManagedByValue:                   "test-app-manager",
	}}

	if err := manager.Orphan(context.Background(), c, existing, &owner); err != nil {
		t.Fatalf("Orphan() error = %v", err)
	}

	retained := configMap("orphaned", nil)
	if err := c.Get(context.Background(), client.ObjectKeyFromObject(retained), retained); err != nil {
		t.Fatalf("getting orphaned resource: %v", err)
	}

	data, found, err := unstructured.NestedStringMap(retained.Object, "data")
	if err != nil || !found || data["requested"] != "value" {
		t.Fatalf("orphaned data = %#v, found=%v, err=%v", data, found, err)
	}
	if len(retained.GetOwnerReferences()) != 0 {
		t.Fatalf("orphaned owner references = %#v, want empty", retained.GetOwnerReferences())
	}
	if retained.GetLabels()["example.com/keep"] != "true" {
		t.Fatalf("unrelated label was removed: %#v", retained.GetLabels())
	}
	for _, key := range []string{
		meta.CreatedByCapsuleLabel,
		meta.NewManagedByCapsuleLabel,
		meta.ProtectedByCapsuleLabel,
		meta.AppManagedByLabel,
	} {
		if _, found := retained.GetLabels()[key]; found {
			t.Fatalf("orphaned resource retains lifecycle label %q", key)
		}
	}
	if _, found := retained.GetAnnotations()[meta.BreakRequestServiceAccountAnnotation]; found {
		t.Fatalf("orphaned resource retains protection ServiceAccount annotation")
	}
	if retained.GetAnnotations()["example.com/keep"] != "true" {
		t.Fatalf("unrelated annotation was removed: %#v", retained.GetAnnotations())
	}
}

type recordingClient struct {
	client.Client
	patchObjects  []client.Object
	deleteObjects []client.Object
}

type recordedPatch struct {
	object    *unstructured.Unstructured
	patchType types.PatchType
	options   client.PatchOptions
}

type applyingClient struct {
	client.Client
	patches []recordedPatch
}

func (c *applyingClient) Patch(
	ctx context.Context,
	obj client.Object,
	patch client.Patch,
	opts ...client.PatchOption,
) error {
	options := client.PatchOptions{}
	for _, opt := range opts {
		opt.ApplyToPatch(&options)
	}

	unstructuredObject := obj.(*unstructured.Unstructured)
	c.patches = append(c.patches, recordedPatch{
		object:    unstructuredObject.DeepCopy(),
		patchType: patch.Type(),
		options:   options,
	})

	if patch.Type() != types.ApplyPatchType {
		return nil
	}

	return c.Client.Create(ctx, unstructuredObject.DeepCopy())
}

func (c *recordingClient) Patch(
	_ context.Context,
	obj client.Object,
	_ client.Patch,
	_ ...client.PatchOption,
) error {
	c.patchObjects = append(c.patchObjects, obj.DeepCopyObject().(client.Object))

	return nil
}

func (c *recordingClient) Delete(
	_ context.Context,
	obj client.Object,
	_ ...client.DeleteOption,
) error {
	c.deleteObjects = append(c.deleteObjects, obj.DeepCopyObject().(client.Object))

	return nil
}

func configMap(name string, data map[string]any) *unstructured.Unstructured {
	obj := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "v1",
		"kind":       "ConfigMap",
		"metadata": map[string]any{
			"name":      name,
			"namespace": "default",
		},
	}}
	if data != nil {
		obj.Object["data"] = data
	}

	return obj
}

func managedField(manager string) metav1.ManagedFieldsEntry {
	return metav1.ManagedFieldsEntry{
		Manager:    manager,
		Operation:  metav1.ManagedFieldsOperationApply,
		APIVersion: "v1",
		FieldsType: "FieldsV1",
		FieldsV1:   &metav1.FieldsV1{Raw: []byte(`{}`)},
	}
}

func assertPatchValue(t *testing.T, patches []clt.JSONPatch, path string, value any) {
	t.Helper()

	for _, patch := range patches {
		if patch.Path == path {
			if patch.Value != value {
				t.Fatalf("patch %q value = %#v, want %#v", path, patch.Value, value)
			}

			return
		}
	}

	t.Fatalf("patch %q not found in %#v", path, patches)
}

func assertNoPatchPath(t *testing.T, patches []clt.JSONPatch, path string) {
	t.Helper()

	for _, patch := range patches {
		if patch.Path == path {
			t.Fatalf("unexpected patch %q in %#v", path, patches)
		}
	}
}
