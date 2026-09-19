// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package resources

import (
	"context"
	"reflect"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	k8smeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api"
	"github.com/projectcapsule/capsule/pkg/api/processor"
	"github.com/projectcapsule/capsule/pkg/runtime/sanitize"
	tpl "github.com/projectcapsule/capsule/pkg/template"
	"github.com/projectcapsule/capsule/pkg/tenant"
	"github.com/projectcapsule/capsule/pkg/utils"
)

func TestCollectorOnlyLoadsContextForGenerators(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name      string
		raw       bool
		generator bool
	}{
		{name: "copied resources only"},
		{name: "raw items", raw: true},
		{name: "generators", generator: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			mapper := k8smeta.NewDefaultRESTMapper([]schema.GroupVersion{{Version: "v1"}})
			mapper.Add(schema.GroupVersionKind{Version: "v1", Kind: "ConfigMap"}, k8smeta.RESTScopeNamespace)
			c := fake.NewClientBuilder().Build()
			collector := NewCollector(c, mapper)
			tnt := &capsulev1beta2.Tenant{Name: "tenant"}
			ns := &corev1.Namespace{Name: "target"}
			spec := capsulev1beta2.ResourceSpec{
				Context: &tpl.TemplateContext{Resources: []*tpl.TemplateResourceReference{{
					APIVersion: "v1", Kind: "ConfigMap",
					Name: "missing-context",
				}}},
			}
			if test.raw {
				spec.RawItems = []capsulev1beta2.RawExtension{{
					Raw: []byte(`{"apiVersion":"v1","kind":"ConfigMap","metadata":{"name":"{{tenant.name}}-{{namespace}}"}}`)}}
			}
			if test.generator {
				spec.Generators = []capsulev1beta2.TemplateItemSpec{{
					Template: `{"apiVersion":"v1","kind":"ConfigMap","metadata":{"name":"generated"}}`,
				}}
			}

			acc := processor.Accumulator{}
			err := collector.Collect(t.Context(), c, CollectorOptions{
				Accumulator: acc,
				Iterator:    NewCollectorIteratorOptions(tnt, ns, spec),
			}, tnt, "0", spec, ns)
			if test.generator {
				if err == nil || !strings.Contains(err.Error(), "missing-context") {
					t.Fatalf("expected missing generator context error, got %v", err)
				}
			} else if err != nil {
				t.Fatalf("unused context prevented collection: %v", err)
			}
			if test.raw {
				if len(acc) != 1 {
					t.Fatalf("accumulated %d objects, want 1", len(acc))
				}
				for _, item := range acc {
					obj := (*item.Objects)[0].Object
					if obj.GetName() != "tenant-target" || obj.GetNamespace() != "target" {
						t.Fatalf("raw item fast context was not applied: %v", obj)
					}
				}
			} else if len(acc) != 0 {
				t.Fatalf("accumulated %d objects, want none", len(acc))
			}
		})
	}
}

func TestCollectorTemplateContextPreservesInputs(t *testing.T) {
	t.Parallel()

	tnt := &capsulev1beta2.Tenant{ObjectMeta: replicationObjectMeta("tenant", "")}
	tnt.Status.Namespaces = []string{"target"}
	ns := &corev1.Namespace{
		ObjectMeta: replicationObjectMeta("target", ""),
		Status:     corev1.NamespaceStatus{Phase: corev1.NamespaceActive},
	}
	originalTenant, originalNamespace := tnt.DeepCopy(), ns.DeepCopy()
	c := fake.NewClientBuilder().Build()
	collector := NewCollector(c, nil)

	// Preserve the previous context's complete shape, including status, UID and RBAC.
	wantTenant, err := tenant.NewTenantContext(tnt, c.Scheme(), collector.contextSanitizeOptions)
	if err != nil {
		t.Fatal(err)
	}
	cleanNamespace := ns.DeepCopy()
	if err := sanitize.SanitizeObject(cleanNamespace, c.Scheme(), collector.contextSanitizeOptions); err != nil {
		t.Fatal(err)
	}
	wantNamespace, err := utils.ToUnstructuredMap(cleanNamespace)
	if err != nil {
		t.Fatal(err)
	}

	for range 2 {
		context, err := collector.gatherTemplateContext(t.Context(), c, CollectorOptions{}, tnt, capsuleResourceSpec(), ns)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(context["tenant"], wantTenant) || !reflect.DeepEqual(context["namespace"], wantNamespace) {
			t.Fatalf("context changed: %#v", context)
		}
		// Template functions can mutate maps. Neither input objects nor the next target's
		// context should observe those changes.
		for _, key := range []string{"tenant", "namespace"} {
			metadata := context[key].(map[string]any)["metadata"].(map[string]any)
			metadata["labels"].(map[string]any)["company.example/team"] = "changed"
			metadata["annotations"].(map[string]any)["company.example/source"] = "changed"
		}
		if !reflect.DeepEqual(tnt, originalTenant) || !reflect.DeepEqual(ns, originalNamespace) {
			t.Fatal("context construction or mutation modified input objects")
		}
	}
}

func TestCollectorReplicasAreSanitizedAndIndependent(t *testing.T) {
	t.Parallel()

	mapper := k8smeta.NewDefaultRESTMapper([]schema.GroupVersion{{Version: "v1"}})
	mapper.Add(schema.GroupVersionKind{Version: "v1", Kind: "ConfigMap"}, k8smeta.RESTScopeNamespace)
	source := newUnstructured("v1", "ConfigMap", "source", "example")
	source.SetUID("source-uid")
	source.SetResourceVersion("7")
	source.SetGeneration(3)
	source.SetLabels(map[string]string{"selected": "true", "keep": "original"})
	source.SetAnnotations(map[string]string{
		"keep": "original",
		"kubectl.kubernetes.io/last-applied-configuration": "source-config",
	})
	source.SetOwnerReferences([]metav1.OwnerReference{{APIVersion: "v1", Kind: "Secret", Name: "owner", UID: "owner-uid"}})
	source.SetManagedFields([]metav1.ManagedFieldsEntry{{
		Manager: "kubectl", Operation: metav1.ManagedFieldsOperationUpdate, APIVersion: "v1",
		FieldsType: "FieldsV1", FieldsV1: &metav1.FieldsV1{Raw: []byte(`{"f:data":{"f:config":{}}}`)},
	}})
	source.Object["data"] = map[string]any{"config": "original"}
	source.Object["status"] = map[string]any{"ready": true}
	original := source.DeepCopy()
	c := fake.NewClientBuilder().WithObjects(source).WithReturnManagedFields().Build()
	storedBefore := newUnstructured("v1", "ConfigMap", "source", "example")
	if err := c.Get(t.Context(), client.ObjectKeyFromObject(source), storedBefore); err != nil {
		t.Fatal(err)
	}
	collector := NewCollector(c, mapper)
	tnt := capsulev1beta2.Tenant{Name: "tenant"}
	ref := tpl.ResourceReference{
		APIVersion: "v1", Kind: "ConfigMap",
		Name:      source.GetName(),
		Namespace: source.GetNamespace(),
		Selector:  &metav1.LabelSelector{MatchLabels: map[string]string{"selected": "true"}},
	}
	spec := capsulev1beta2.ResourceSpec{
		NamespacedItems: []tpl.ResourceReference{ref, ref},
		AdditionalMetadata: &api.AdditionalMetadataSpec{
			Labels: map[string]string{"target": "{{namespace}}"},
		},
	}
	opts := CollectorOptions{Accumulator: processor.Accumulator{}, AllowCrossNamespaceSelection: true}
	sources, err := collector.CollectNamespacedItems(t.Context(), c, opts, spec, nil, tnt)
	if err != nil {
		t.Fatal(err)
	}
	if len(sources) != 1 {
		t.Fatalf("collected %d sources, want 1", len(sources))
	}
	for _, obj := range sources {
		assertSanitizedReplica(t, obj)
		if obj.GetNamespace() != "source" || obj.GetLabels()["selected"] != "" {
			t.Fatalf("unexpected source identity or selection labels: %v", obj)
		}
	}

	opts.AllowCrossNamespaceSelection = false
	for _, name := range []string{"source", "target-a", "target-b"} {
		if err := collector.CollectForNamespace(t.Context(), c, opts, tnt, "0", spec, sources,
			&corev1.Namespace{Name: name}); err != nil {
			t.Fatal(err)
		}
	}
	if len(opts.Accumulator) != 2 {
		t.Fatalf("accumulated %d replicas, want 2 (skip source namespace)", len(opts.Accumulator))
	}
	for _, item := range opts.Accumulator {
		obj := (*item.Objects)[0].Object
		assertSanitizedReplica(t, obj)
		if obj.GetLabels()["target"] != obj.GetNamespace() || obj.GetLabels()["keep"] != "original" ||
			obj.Object["data"].(map[string]any)["config"] != "original" {
			t.Fatalf("replica has incorrect or shared data: %v", obj)
		}
		obj.Object["data"].(map[string]any)["config"] = "changed"
		obj.Object["metadata"].(map[string]any)["labels"].(map[string]any)["keep"] = "changed"
	}
	for _, obj := range sources {
		if obj.Object["data"].(map[string]any)["config"] != "original" || obj.GetLabels()["keep"] != "original" {
			t.Fatal("replica mutation modified the collected source")
		}
	}
	stored := newUnstructured("v1", "ConfigMap", "source", "example")
	if err := c.Get(t.Context(), client.ObjectKeyFromObject(source), stored); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(stored, storedBefore) || !reflect.DeepEqual(source, original) {
		t.Fatal("collection modified the original source")
	}
}

func assertSanitizedReplica(t *testing.T, obj *unstructured.Unstructured) {
	t.Helper()
	for _, key := range []string{"uid", "resourceVersion", "generation", "ownerReferences", "managedFields"} {
		if _, exists := obj.Object["metadata"].(map[string]any)[key]; exists {
			t.Errorf("replica still contains metadata.%s", key)
		}
	}
	if _, exists := obj.Object["status"]; exists {
		t.Error("replica still contains status")
	}
	annotations := obj.GetAnnotations()
	if _, exists := annotations["kubectl.kubernetes.io/last-applied-configuration"]; exists {
		t.Error("replica still contains last-applied annotation")
	}
	if annotations["keep"] != "original" {
		t.Errorf("replica lost user annotation: %v", annotations)
	}
}

func TestGatherAdditionalMetadataDoesNotMutateSpec(t *testing.T) {
	t.Parallel()

	spec := capsulev1beta2.ResourceSpec{AdditionalMetadata: &api.AdditionalMetadataSpec{
		Labels:      map[string]string{"target-{{namespace}}": "{{tenant.name}}"},
		Annotations: map[string]string{"target": "{{namespace}}"},
	}}
	original := spec.DeepCopy()
	for _, ns := range []string{"a", "b"} {
		labels, annotations := GatherAdditionalMetadata(spec, map[string]string{"namespace": ns, "tenant.name": "tenant"})
		if labels["target-"+ns] != "tenant" || annotations["target"] != ns {
			t.Fatalf("unexpected metadata: %v, %v", labels, annotations)
		}
		labels["mutated"] = "true"
		annotations["mutated"] = "true"
		if !reflect.DeepEqual(spec, *original) {
			t.Fatal("metadata templating or mutation modified the spec")
		}
	}
}

func TestCollectorAddToAccumulationClusterScopedObjects(t *testing.T) {
	t.Parallel()

	mapper := k8smeta.NewDefaultRESTMapper([]schema.GroupVersion{{Version: "v1"}})
	mapper.Add(schema.GroupVersionKind{Version: "v1", Kind: "ConfigMap"}, k8smeta.RESTScopeNamespace)
	mapper.Add(schema.GroupVersionKind{Version: "v1", Kind: "Namespace"}, k8smeta.RESTScopeRoot)

	collector := NewCollector(nil, mapper)

	t.Run("allows namespaced object", func(t *testing.T) {
		t.Parallel()

		acc := processor.Accumulator{}
		obj := newUnstructured("v1", "ConfigMap", "default", "example")

		if err := collector.AddToAccumulation(nil, nil, CollectorOptions{Accumulator: acc}, capsuleResourceSpec(), obj, "test", true); err != nil {
			t.Fatalf("expected no error, got %v", err)
		}

		if len(acc) != 1 {
			t.Fatalf("expected object to be accumulated, got %d items", len(acc))
		}
	})

	t.Run("rejects cluster scoped object by default", func(t *testing.T) {
		t.Parallel()

		acc := processor.Accumulator{}
		obj := newUnstructured("v1", "Namespace", "", "example")

		err := collector.AddToAccumulation(nil, nil, CollectorOptions{Accumulator: acc}, capsuleResourceSpec(), obj, "test", true)
		if err == nil {
			t.Fatal("expected error, got nil")
		}

		if !strings.Contains(err.Error(), "cluster-scoped kind v1/Namespace is not allowed") {
			t.Fatalf("expected cluster scoped error, got %v", err)
		}

		if len(acc) != 0 {
			t.Fatalf("expected object not to be accumulated, got %d items", len(acc))
		}
	})

	t.Run("allows cluster scoped object when configured", func(t *testing.T) {
		t.Parallel()

		acc := processor.Accumulator{}
		obj := newUnstructured("v1", "Namespace", "", "example")

		opts := CollectorOptions{
			Accumulator:               acc,
			AllowClusterScopedObjects: true,
		}

		if err := collector.AddToAccumulation(nil, nil, opts, capsuleResourceSpec(), obj, "test", true); err != nil {
			t.Fatalf("expected no error, got %v", err)
		}

		if len(acc) != 1 {
			t.Fatalf("expected object to be accumulated, got %d items", len(acc))
		}
	})

	t.Run("keeps allowing namespaced object targeting a namespace", func(t *testing.T) {
		t.Parallel()

		acc := processor.Accumulator{}
		obj := newUnstructured("v1", "ConfigMap", "source", "example")

		opts := CollectorOptions{
			Accumulator:               acc,
			AllowClusterScopedObjects: true,
		}

		target := &corev1.Namespace{Name: "tenant-a"}

		if err := collector.AddToAccumulation(nil, target, opts, capsuleResourceSpec(), obj, "test", true); err != nil {
			t.Fatalf("expected no error, got %v", err)
		}

		if len(acc) != 1 {
			t.Fatalf("expected object to be accumulated, got %d items", len(acc))
		}
	})
}

func TestCollectorAddsReplicationMetadataToGeneratorContext(t *testing.T) {
	t.Parallel()

	mapper := k8smeta.NewDefaultRESTMapper([]schema.GroupVersion{{Version: "v1"}})
	mapper.Add(schema.GroupVersionKind{Version: "v1", Kind: "ConfigMap"}, k8smeta.RESTScopeNamespace)

	replicationContext, err := newReplicationContext(&capsulev1beta2.TenantResource{
		Name:      "tenant-distribution",
		Namespace: "solar-system",
	})
	if err != nil {
		t.Fatalf("newReplicationContext() error = %v", err)
	}

	acc := processor.Accumulator{}
	collector := NewCollector(nil, mapper)
	spec := capsulev1beta2.ResourceSpec{
		Generators: []capsulev1beta2.TemplateItemSpec{{
			MissingKey: "error",
			Template: `apiVersion: v1
kind: ConfigMap
metadata:
  name: {{ $.replications.metadata.name }}
  annotations:
    replication-namespace: {{ $.replications.metadata.namespace }}
`,
		}},
	}

	err = collector.Collect(
		context.Background(),
		nil,
		CollectorOptions{
			Accumulator:        acc,
			ReplicationContext: replicationContext,
		},
		nil,
		"0",
		spec,
		nil,
	)
	if err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	if len(acc) != 1 {
		t.Fatalf("Collect() accumulated %d objects, want 1", len(acc))
	}

	for _, item := range acc {
		if item == nil || item.Objects == nil || len(*item.Objects) != 1 {
			t.Fatalf("accumulated item = %#v", item)
		}

		object := (*item.Objects)[0].Object
		if object.GetName() != "tenant-distribution" {
			t.Fatalf("generated name = %q", object.GetName())
		}
		if object.GetAnnotations()["replication-namespace"] != "solar-system" {
			t.Fatalf("generated annotations = %#v", object.GetAnnotations())
		}
	}
}

func TestCollectorPreservesOwnerReferencesInAuthoredResources(t *testing.T) {
	t.Parallel()

	mapper := k8smeta.NewDefaultRESTMapper([]schema.GroupVersion{{Version: "v1"}})
	mapper.Add(schema.GroupVersionKind{Version: "v1", Kind: "ConfigMap"}, k8smeta.RESTScopeNamespace)

	const objectTemplate = `apiVersion: v1
kind: ConfigMap
metadata:
  name: %s
  ownerReferences:
    - apiVersion: capsule.clastix.io/v1beta2
      kind: TenantResource
      name: tenant-distribution
      uid: replication-uid
      controller: true
      blockOwnerDeletion: true
`

	spec := capsulev1beta2.ResourceSpec{
		RawItems: []capsulev1beta2.RawExtension{{
			Raw: []byte(`{
  "apiVersion": "v1",
  "kind": "ConfigMap",
  "metadata": {
    "name": "raw-item",
    "ownerReferences": [{
      "apiVersion": "capsule.clastix.io/v1beta2",
      "kind": "TenantResource",
      "name": "tenant-distribution",
      "uid": "replication-uid",
      "controller": true,
      "blockOwnerDeletion": true
    }]
  }
}`),
		}},
		Generators: []capsulev1beta2.TemplateItemSpec{{
			MissingKey: "error",
			Template:   strings.Replace(objectTemplate, "%s", "generated-item", 1),
		}},
	}

	acc := processor.Accumulator{}
	collector := NewCollector(nil, mapper)

	if err := collector.Collect(
		context.Background(),
		nil,
		CollectorOptions{Accumulator: acc},
		nil,
		"0",
		spec,
		nil,
	); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	if len(acc) != 2 {
		t.Fatalf("Collect() accumulated %d objects, want 2", len(acc))
	}

	for _, item := range acc {
		if item == nil || item.Objects == nil || len(*item.Objects) != 1 {
			t.Fatalf("accumulated item = %#v", item)
		}

		object := (*item.Objects)[0].Object
		ownerReferences := object.GetOwnerReferences()
		if len(ownerReferences) != 1 {
			t.Fatalf("%s ownerReferences = %#v, want one", object.GetName(), ownerReferences)
		}

		owner := ownerReferences[0]
		if owner.APIVersion != "capsule.clastix.io/v1beta2" ||
			owner.Kind != "TenantResource" ||
			owner.Name != "tenant-distribution" ||
			owner.UID != "replication-uid" ||
			owner.Controller == nil || !*owner.Controller ||
			owner.BlockOwnerDeletion == nil || !*owner.BlockOwnerDeletion {
			t.Fatalf("%s ownerReference = %#v", object.GetName(), owner)
		}
	}
}

func TestCollectorStripsOwnerReferencesFromReplicatedResources(t *testing.T) {
	t.Parallel()

	mapper := k8smeta.NewDefaultRESTMapper([]schema.GroupVersion{{Version: "v1"}})
	mapper.Add(schema.GroupVersionKind{Version: "v1", Kind: "ConfigMap"}, k8smeta.RESTScopeNamespace)

	obj := newUnstructured("v1", "ConfigMap", "source", "replicated-item")
	obj.SetOwnerReferences([]metav1.OwnerReference{{
		APIVersion: "apps/v1",
		Kind:       "Deployment",
		Name:       "source-owner",
		UID:        "source-owner-uid",
	}})

	acc := processor.Accumulator{}
	collector := NewCollector(nil, mapper)

	if err := collector.AddToAccumulation(
		nil,
		nil,
		CollectorOptions{Accumulator: acc},
		capsuleResourceSpec(),
		obj,
		"replica",
		false,
	); err != nil {
		t.Fatalf("AddToAccumulation() error = %v", err)
	}
	if ownerReferences := obj.GetOwnerReferences(); len(ownerReferences) != 0 {
		t.Fatalf("ownerReferences = %#v, want none", ownerReferences)
	}
}

func newUnstructured(apiVersion, kind, namespace, name string) *unstructured.Unstructured {
	obj := &unstructured.Unstructured{}
	obj.SetAPIVersion(apiVersion)
	obj.SetKind(kind)
	obj.SetNamespace(namespace)
	obj.SetName(name)

	return obj
}

func capsuleResourceSpec() capsulev1beta2.ResourceSpec {
	return capsulev1beta2.ResourceSpec{}
}
