// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package resources

import (
	"context"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	k8smeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/processor"
)

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

		target := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "tenant-a"}}

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
		ObjectMeta: metav1.ObjectMeta{
			Name:      "tenant-distribution",
			Namespace: "solar-system",
		},
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
			RawExtension: runtime.RawExtension{Raw: []byte(`{
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
}`)},
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
