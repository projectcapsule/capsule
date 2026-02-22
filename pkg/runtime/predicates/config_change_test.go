// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package predicates_test

import (
	"testing"

	"sigs.k8s.io/controller-runtime/pkg/event"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api"
	"github.com/projectcapsule/capsule/pkg/runtime/predicates"
)

func TestCapsuleConfigSpecChangedPredicate_StaticFuncs(t *testing.T) {
	t.Parallel()

	p := predicates.CapsuleConfigSpecChangedPredicate{}

	if got := p.Create(event.CreateEvent{}); got {
		t.Fatalf("Create() = %v, want false", got)
	}
	if got := p.Delete(event.DeleteEvent{}); got {
		t.Fatalf("Delete() = %v, want false", got)
	}
	if got := p.Generic(event.GenericEvent{}); got {
		t.Fatalf("Generic() = %v, want false", got)
	}
}

func TestCapsuleConfigSpecChangedPredicate_Update(t *testing.T) {
	t.Parallel()

	p := predicates.CapsuleConfigSpecChangedPredicate{}

	t.Run("returns false when types are not CapsuleConfiguration", func(t *testing.T) {
		t.Parallel()

		ev := event.UpdateEvent{
			ObjectOld: &capsulev1beta2.GlobalTenantResource{},
			ObjectNew: &capsulev1beta2.GlobalTenantResource{},
		}

		if got := p.Update(ev); got {
			t.Fatalf("Update() = %v, want false", got)
		}
	})

	t.Run("returns false when administrators length unchanged", func(t *testing.T) {
		t.Parallel()

		oldObj := &capsulev1beta2.CapsuleConfiguration{}
		newObj := &capsulev1beta2.CapsuleConfiguration{}

		// same length (0)
		ev := event.UpdateEvent{ObjectOld: oldObj, ObjectNew: newObj}
		if got := p.Update(ev); got {
			t.Fatalf("Update() = %v, want false", got)
		}

		// same length (2)
		oldObj.Spec.Administrators = []api.UserSpec{
			{Name: "a"},
			{Name: "b"},
		}

		newObj.Spec.Administrators = []api.UserSpec{
			{Name: "x"},
			{Name: "y"},
		}

		ev = event.UpdateEvent{ObjectOld: oldObj, ObjectNew: newObj}
		if got := p.Update(ev); got {
			t.Fatalf("Update() = %v, want false", got)
		}
	})

	t.Run("returns true when administrators length changed", func(t *testing.T) {
		t.Parallel()

		oldObj := &capsulev1beta2.CapsuleConfiguration{}
		newObj := &capsulev1beta2.CapsuleConfiguration{}

		oldObj.Spec.Administrators = []api.UserSpec{
			{Name: "a"},
		}

		newObj.Spec.Administrators = []api.UserSpec{
			{Name: "a"},
			{Name: "b"},
		}

		ev := event.UpdateEvent{ObjectOld: oldObj, ObjectNew: newObj}
		if got := p.Update(ev); !got {
			t.Fatalf("Update() = %v, want true", got)
		}
	})
}
