// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package meta_test

import (
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/runtime/gvk"
)

func mkItem(tenant, namespace, name, kind string, status metav1.ConditionStatus, condType, msg string, created bool, lastApply metav1.Time) meta.ObjectReferenceStatus {
	return meta.ObjectReferenceStatus{
		ResourceID: gvk.ResourceID{
			TenantResourceIDWithOrigin: gvk.TenantResourceIDWithOrigin{
				TenantResourceID: gvk.TenantResourceID{Tenant: tenant},
				Origin:           "",
			},
			Group:     "",
			Version:   "",
			Kind:      kind,
			Name:      name,
			Namespace: namespace,
		},
		ObjectReferenceStatusCondition: meta.ObjectReferenceStatusCondition{
			Status:    status,
			Type:      condType,
			Message:   msg,
			LastApply: lastApply,
			Created:   created,
		},
	}
}

func TestProcessedItems_UpdateItem_AppendsNew(t *testing.T) {
	var p meta.ProcessedItems

	now := metav1.NewTime(time.Now())

	item := mkItem("t1", "ns1", "name1", "Secret", metav1.ConditionTrue, "Ready", "ok", true, now)
	p.UpdateItem(item)

	if len(p) != 1 {
		t.Fatalf("expected 1 item, got %d", len(p))
	}
	if p[0].ResourceID != item.ResourceID {
		t.Fatalf("expected ResourceID %+v, got %+v", item.ResourceID, p[0].ResourceID)
	}
	if p[0].ObjectReferenceStatusCondition != item.ObjectReferenceStatusCondition {
		t.Fatalf("expected condition %+v, got %+v", item.ObjectReferenceStatusCondition, p[0].ObjectReferenceStatusCondition)
	}
}

func TestProcessedItems_UpdateItem_UpdatesExistingWithoutDuplicate(t *testing.T) {
	now1 := metav1.NewTime(time.Now().Add(-1 * time.Hour))
	now2 := metav1.NewTime(time.Now())

	original := mkItem("t1", "ns1", "name1", "Secret", metav1.ConditionFalse, "Ready", "initial", false, now1)
	updated := mkItem("t1", "ns1", "name1", "Secret", metav1.ConditionTrue, "Ready", "updated", true, now2)

	p := meta.ProcessedItems{original}
	p.UpdateItem(updated)

	if len(p) != 1 {
		t.Fatalf("expected 1 item (updated in place), got %d", len(p))
	}

	// Resource identity must remain same
	if p[0].ResourceID != original.ResourceID {
		t.Fatalf("expected ResourceID unchanged %+v, got %+v", original.ResourceID, p[0].ResourceID)
	}

	// Condition should be replaced
	if p[0].ObjectReferenceStatusCondition != updated.ObjectReferenceStatusCondition {
		t.Fatalf("expected condition replaced with %+v, got %+v", updated.ObjectReferenceStatusCondition, p[0].ObjectReferenceStatusCondition)
	}
}

func TestProcessedItems_RemoveItem_RemovesMatchingByResourceID(t *testing.T) {
	now := metav1.NewTime(time.Now())

	a := mkItem("t1", "ns1", "name1", "Secret", metav1.ConditionTrue, "Ready", "a", true, now)
	b := mkItem("t1", "ns1", "name2", "ConfigMap", metav1.ConditionTrue, "Ready", "b", true, now)
	c := mkItem("t2", "ns9", "name9", "Secret", metav1.ConditionFalse, "Ready", "c", false, now)

	p := meta.ProcessedItems{a, b, c}

	// Remove b
	p.RemoveItem(b)

	if len(p) != 2 {
		t.Fatalf("expected 2 items after removal, got %d", len(p))
	}

	// Ensure b is gone, others remain
	for _, it := range p {
		if it.ResourceID == b.ResourceID {
			t.Fatalf("did not expect removed item %+v to remain", b.ResourceID)
		}
	}
	// Ensure a and c still exist
	foundA, foundC := false, false
	for _, it := range p {
		if it.ResourceID == a.ResourceID {
			foundA = true
		}
		if it.ResourceID == c.ResourceID {
			foundC = true
		}
	}
	if !foundA || !foundC {
		t.Fatalf("expected remaining items to include a=%v c=%v", foundA, foundC)
	}
}

func TestProcessedItems_GetItem_ReturnsPointerToSliceElement(t *testing.T) {
	now := metav1.NewTime(time.Now())

	a := mkItem("t1", "ns1", "name1", "Secret", metav1.ConditionFalse, "Ready", "a", false, now)
	b := mkItem("t1", "ns1", "name2", "Secret", metav1.ConditionFalse, "Ready", "b", false, now)

	p := meta.ProcessedItems{a, b}

	ptr := p.GetItem(b.ResourceID)
	if ptr == nil {
		t.Fatalf("expected to find item %v", b.ResourceID)
	}

	// Mutate through pointer and ensure slice reflects it (proves it's not a copy)
	ptr.Message = "changed"
	ptr.Status = metav1.ConditionTrue
	ptr.Created = true

	if p[1].Message != "changed" {
		t.Fatalf("expected slice element Message to be changed, got %q", p[1].Message)
	}
	if p[1].Status != metav1.ConditionTrue {
		t.Fatalf("expected slice element Status to be changed, got %q", p[1].Status)
	}
	if p[1].Created != true {
		t.Fatalf("expected slice element Created to be changed, got %v", p[1].Created)
	}

	// Not found case
	none := p.GetItem(gvk.ResourceID{Name: "does-not-exist"})
	if none != nil {
		t.Fatalf("expected nil for non-existent item, got %+v", none)
	}
}

func TestProcessedItems_SortDeterministic(t *testing.T) {
	now := metav1.NewTime(time.Now())

	// Intentionally shuffled order
	i1 := mkItem("tenant-b", "ns-a", "name-a", "Secret", metav1.ConditionTrue, "Ready", "", true, now)
	i2 := mkItem("tenant-a", "ns-b", "name-a", "Secret", metav1.ConditionTrue, "Ready", "", true, now)
	i3 := mkItem("tenant-a", "ns-a", "name-b", "Secret", metav1.ConditionTrue, "Ready", "", true, now)
	i4 := mkItem("tenant-a", "ns-a", "name-a", "ZKind", metav1.ConditionTrue, "Ready", "", true, now)
	i5 := mkItem("tenant-a", "ns-a", "name-a", "AKind", metav1.ConditionTrue, "Ready", "", true, now)
	i6 := mkItem("tenant-b", "ns-a", "name-a", "ConfigMap", metav1.ConditionTrue, "Ready", "", true, now)

	p := meta.ProcessedItems{i1, i2, i3, i4, i5, i6}
	p.SortDeterministic()

	// Expected order by: Tenant, Namespace, Name, Kind
	want := []gvk.ResourceID{
		i5.ResourceID, // tenant-a, ns-a, name-a, AKind
		i4.ResourceID, // tenant-a, ns-a, name-a, ZKind
		i3.ResourceID, // tenant-a, ns-a, name-b, Secret
		i2.ResourceID, // tenant-a, ns-b, name-a, Secret
		i6.ResourceID, // tenant-b, ns-a, name-a, ConfigMap
		i1.ResourceID, // tenant-b, ns-a, name-a, Secret
	}

	if len(p) != len(want) {
		t.Fatalf("expected %d items, got %d", len(want), len(p))
	}

	for idx := range want {
		if p[idx].ResourceID != want[idx] {
			t.Fatalf("at index %d: expected %v, got %v", idx, want[idx], p[idx].ResourceID)
		}
	}
}
