// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package processor

import (
	"errors"
	"testing"

	k8smeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/runtime/gvk"
)

func TestObjectForProcessedItem(t *testing.T) {
	t.Parallel()

	mapper := k8smeta.NewDefaultRESTMapper([]schema.GroupVersion{{Version: "v1"}})
	mapper.Add(schema.GroupVersionKind{Version: "v1", Kind: "Secret"}, k8smeta.RESTScopeNamespace)
	mapper.Add(schema.GroupVersionKind{Version: "v1", Kind: "Namespace"}, k8smeta.RESTScopeRoot)

	p := &Processor{Mapper: mapper}

	t.Run("keeps namespace for namespaced resource", func(t *testing.T) {
		t.Parallel()

		obj, err := p.objectForProcessedItem(meta.ObjectReferenceStatus{
			ResourceID: gvk.ResourceID{
				Version:   "v1",
				Kind:      "Secret",
				Namespace: "tenant-a",
				Name:      "example",
			},
		})
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}

		if obj.GetNamespace() != "tenant-a" {
			t.Fatalf("expected namespace tenant-a, got %q", obj.GetNamespace())
		}
	})

	t.Run("drops tracking namespace for mapped cluster scoped resource", func(t *testing.T) {
		t.Parallel()

		obj, err := p.objectForProcessedItem(meta.ObjectReferenceStatus{
			ResourceID: gvk.ResourceID{
				Version:   "v1",
				Kind:      "Namespace",
				Namespace: "tenant-a",
				Name:      "example",
			},
		})
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}

		if obj.GetNamespace() != "" {
			t.Fatalf("expected empty namespace, got %q", obj.GetNamespace())
		}
	})

	t.Run("uses status flag without mapper lookup", func(t *testing.T) {
		t.Parallel()

		obj, err := (&Processor{}).objectForProcessedItem(meta.ObjectReferenceStatus{
			ResourceID: gvk.ResourceID{
				Version:   "v1",
				Kind:      "UnknownClusterKind",
				Namespace: "tenant-a",
				Name:      "example",
			},
			ObjectReferenceStatusCondition: meta.ObjectReferenceStatusCondition{
				ClusterScoped: true,
			},
		})
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}

		if obj.GetNamespace() != "" {
			t.Fatalf("expected empty namespace, got %q", obj.GetNamespace())
		}
	})
}

func TestFailAndRecord(t *testing.T) {
	t.Parallel()

	processed := meta.ProcessedItems{}
	itemErrors := 0
	item := meta.ObjectReferenceStatus{
		ResourceID: gvk.ResourceID{
			Version: "v1",
			Kind:    "Secret",
			Name:    "example",
		},
		ObjectReferenceStatusCondition: meta.ObjectReferenceStatusCondition{
			Status: metav1.ConditionTrue,
		},
	}

	if failAndRecord(&processed, &itemErrors, item, "prefix: ", nil) {
		t.Fatal("expected nil error to be ignored")
	}

	if itemErrors != 0 {
		t.Fatalf("expected no item errors, got %d", itemErrors)
	}

	if failAndRecord(&processed, &itemErrors, item, "prefix: ", errors.New("boom")) != true {
		t.Fatal("expected error to be recorded")
	}

	if itemErrors != 1 {
		t.Fatalf("expected one item error, got %d", itemErrors)
	}

	got := processed.GetItem(item.ResourceID)
	if got == nil {
		t.Fatal("expected processed item to be recorded")
	}

	if got.Status != metav1.ConditionFalse {
		t.Fatalf("expected status False, got %q", got.Status)
	}

	if got.Message != "prefix: boom" {
		t.Fatalf("expected message %q, got %q", "prefix: boom", got.Message)
	}
}
