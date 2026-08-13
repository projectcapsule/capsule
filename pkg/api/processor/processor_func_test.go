// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package processor

import (
	"context"
	"errors"
	"testing"

	"github.com/go-logr/logr"
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

func TestProcessorScopeMatches(t *testing.T) {
	t.Parallel()

	scope := Scope{Tenant: "tenant-a", Namespace: "ns-a"}

	for _, tc := range []struct {
		name string
		id   gvk.ResourceID
		want bool
	}{
		{
			name: "same tenant and namespace",
			id:   resourceID("tenant-a", "ns-a", "settings"),
			want: true,
		},
		{
			name: "other namespace",
			id:   resourceID("tenant-a", "ns-b", "settings"),
			want: false,
		},
		{
			name: "other tenant",
			id:   resourceID("tenant-b", "ns-a", "settings"),
			want: false,
		},
		{
			name: "no namespace",
			id:   resourceID("tenant-a", "", "settings"),
			want: false,
		},
	} {
		if got := scope.Matches(tc.id); got != tc.want {
			t.Fatalf("%s: expected %v, got %v", tc.name, tc.want, got)
		}
	}
}

func TestScopedAccumulator(t *testing.T) {
	t.Parallel()

	inScope := resourceID("tenant-a", "ns-a", "settings")
	otherNs := resourceID("tenant-a", "ns-b", "settings")
	otherTnt := resourceID("tenant-b", "ns-a", "settings")

	acc := Accumulator{
		inScope.GetKey(""):  {Resource: inScope},
		otherNs.GetKey(""):  {Resource: otherNs},
		otherTnt.GetKey(""): {Resource: otherTnt},
		"empty":             nil,
	}

	scoped, ignored := scopedAccumulator(acc, Scope{Tenant: "tenant-a", Namespace: "ns-a"})

	if len(scoped) != 1 {
		t.Fatalf("expected a single scoped item, got %d", len(scoped))
	}

	if scoped[inScope.GetKey("")] == nil {
		t.Fatal("expected the in scope item to be retained")
	}

	if ignored != 2 {
		t.Fatalf("expected 2 ignored items, got %d", ignored)
	}

	// The nil entry is dropped without being accounted as ignored.
	if _, ok := scoped["empty"]; ok {
		t.Fatal("expected the empty entry to be dropped")
	}
}

func TestReconcileNamespaceRequiresNamespace(t *testing.T) {
	t.Parallel()

	items, err := (&Processor{}).ReconcileNamespace(
		context.Background(),
		logr.Discard(),
		nil,
		nil,
		Accumulator{},
		ProcessorOptions{},
		Scope{Tenant: "tenant-a"},
	)
	if err == nil {
		t.Fatal("expected an error for a scope without a Namespace")
	}

	if items != nil {
		t.Fatalf("expected no processed item, got %+v", items)
	}
}

func TestReconcileNamespaceSeedsScopeOnly(t *testing.T) {
	t.Parallel()

	inScope := resourceID("tenant-a", "ns-a", "settings")
	otherNs := resourceID("tenant-a", "ns-b", "settings")

	current := meta.ProcessedItems{
		{ResourceID: otherNs, ObjectReferenceStatusCondition: meta.ObjectReferenceStatusCondition{Created: true}},
		{ResourceID: inScope, ObjectReferenceStatusCondition: meta.ObjectReferenceStatusCondition{Created: true}},
	}

	// An empty Accumulator reaches out to no client at all: the outcome is only made
	// of what was already tracked for the reconciled scope.
	items, err := (&Processor{}).ReconcileNamespace(
		context.Background(),
		logr.Discard(),
		nil,
		current,
		Accumulator{},
		ProcessorOptions{},
		Scope{Tenant: "tenant-a", Namespace: "ns-a"},
	)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if len(items) != 1 || items[0].ResourceID != inScope {
		t.Fatalf("expected only the in scope item, got %+v", items)
	}

	if !items[0].Created {
		t.Fatal("expected the tracked creation flag to be carried over")
	}

	if len(current) != 2 {
		t.Fatalf("expected the given items to be left untouched, got %+v", current)
	}
}

func resourceID(tenant, namespace, name string) gvk.ResourceID {
	return gvk.ResourceID{
		TenantResourceIDWithOrigin: gvk.TenantResourceIDWithOrigin{
			TenantResourceID: gvk.TenantResourceID{Tenant: tenant},
		},
		Version:   "v1",
		Kind:      "ConfigMap",
		Name:      name,
		Namespace: namespace,
	}
}
