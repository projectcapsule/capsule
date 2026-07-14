// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package predicates_test

import (
	"testing"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/rbac"
	"github.com/projectcapsule/capsule/pkg/runtime/predicates"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/event"
)

func TestTenantStatusOwnersChangedPredicate(t *testing.T) {
	t.Parallel()

	p := predicates.TenantStatusOwnersChangedPredicate{}
	withOwner := &capsulev1beta2.Tenant{Status: capsulev1beta2.TenantStatus{
		Owners: rbac.OwnerStatusListSpec{{UserSpec: rbac.UserSpec{Kind: rbac.UserOwner, Name: "alice"}}},
	}}
	withoutOwner := &capsulev1beta2.Tenant{}

	if !p.Create(event.CreateEvent{Object: withOwner}) {
		t.Fatalf("Create() = false, want true when owners are present")
	}
	if p.Create(event.CreateEvent{Object: withoutOwner}) {
		t.Fatalf("Create() = true, want false without owners")
	}
	if p.Create(event.CreateEvent{Object: &corev1.ConfigMap{}}) {
		t.Fatalf("Create() = true, want false for non-tenant")
	}
	if !p.Delete(event.DeleteEvent{Object: withOwner}) {
		t.Fatalf("Delete() = false, want true when owners are present")
	}
	if !p.Generic(event.GenericEvent{Object: withOwner}) {
		t.Fatalf("Generic() = false, want true when owners are present")
	}

	if !p.Update(event.UpdateEvent{ObjectOld: withoutOwner, ObjectNew: withOwner}) {
		t.Fatalf("Update() = false, want true when owners changed")
	}
	if p.Update(event.UpdateEvent{ObjectOld: withOwner, ObjectNew: withOwner.DeepCopy()}) {
		t.Fatalf("Update() = true, want false when owners are unchanged")
	}
	if p.Update(event.UpdateEvent{ObjectOld: &corev1.ConfigMap{}, ObjectNew: withOwner}) {
		t.Fatalf("Update() = true, want false for non-tenant")
	}
}

func TestTenantCountChangedPredicate(t *testing.T) {
	t.Parallel()

	p := predicates.TenantCountChangedPredicate{}
	if !p.Create(event.CreateEvent{}) {
		t.Fatalf("Create() = false, want true")
	}
	if !p.Delete(event.DeleteEvent{}) {
		t.Fatalf("Delete() = false, want true")
	}
	if p.Generic(event.GenericEvent{}) {
		t.Fatalf("Generic() = true, want false")
	}
	if p.Update(event.UpdateEvent{}) {
		t.Fatalf("Update() = true, want false")
	}
}

func TestNamesMatchingConstructor(t *testing.T) {
	t.Parallel()

	_ = predicates.NamesMatching("a", "b")

	p := predicates.NamesMatchingPredicate{Names: []string{"a", "b"}}
	matching := &corev1.ConfigMap{}
	matching.Name = "a"
	other := &corev1.ConfigMap{}
	other.Name = "c"

	if !p.Create(event.CreateEvent{Object: matching}) {
		t.Fatalf("Create() = false, want true for matching name")
	}
	if p.Create(event.CreateEvent{Object: other}) {
		t.Fatalf("Create() = true, want false for non-matching name")
	}
	if p.Delete(event.DeleteEvent{}) {
		t.Fatalf("Delete() = true, want false for nil object")
	}
	if !p.Update(event.UpdateEvent{ObjectNew: matching}) {
		t.Fatalf("Update() = false, want true for matching new object")
	}
	if !p.Generic(event.GenericEvent{Object: matching}) {
		t.Fatalf("Generic() = false, want true for matching name")
	}
}
