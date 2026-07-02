// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package resources

import (
	"strings"
	"testing"

	k8smeta "k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
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
