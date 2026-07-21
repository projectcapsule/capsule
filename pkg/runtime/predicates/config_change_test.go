// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package predicates_test

import (
	"testing"

	"sigs.k8s.io/controller-runtime/pkg/event"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/rbac"
	"github.com/projectcapsule/capsule/pkg/runtime/predicates"
)

func TestCapsuleConfigSpecAdministratorsChangedPredicate_StaticFuncs(t *testing.T) {
	t.Parallel()

	p := predicates.CapsuleConfigSpecAdministratorsChangedPredicate{}

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

func TestCapsuleConfigSpecAdministratorsChangedPredicate_Update(t *testing.T) {
	t.Parallel()

	p := predicates.CapsuleConfigSpecAdministratorsChangedPredicate{}

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

	t.Run("compares administrator contents", func(t *testing.T) {
		t.Parallel()

		oldObj := &capsulev1beta2.CapsuleConfiguration{}
		newObj := &capsulev1beta2.CapsuleConfiguration{}

		// same length (0)
		ev := event.UpdateEvent{ObjectOld: oldObj, ObjectNew: newObj}
		if got := p.Update(ev); got {
			t.Fatalf("Update() = %v, want false", got)
		}

		// Equal non-empty lists remain filtered.
		oldObj.Spec.Administrators = []rbac.UserSpec{
			{Name: "a"},
			{Name: "b"},
		}
		newObj.Spec.Administrators = oldObj.Spec.Administrators.DeepCopy()
		ev = event.UpdateEvent{ObjectOld: oldObj, ObjectNew: newObj}
		if got := p.Update(ev); got {
			t.Fatalf("Update() = %v, want false", got)
		}

		// Replacements must reconcile even when the list length is unchanged.
		newObj.Spec.Administrators = []rbac.UserSpec{
			{Name: "x"},
			{Name: "y"},
		}

		ev = event.UpdateEvent{ObjectOld: oldObj, ObjectNew: newObj}
		if got := p.Update(ev); !got {
			t.Fatalf("Update() = %v, want true", got)
		}
	})

	t.Run("returns true when administrators length changed", func(t *testing.T) {
		t.Parallel()

		oldObj := &capsulev1beta2.CapsuleConfiguration{}
		newObj := &capsulev1beta2.CapsuleConfiguration{}

		oldObj.Spec.Administrators = []rbac.UserSpec{
			{Name: "a"},
		}

		newObj.Spec.Administrators = []rbac.UserSpec{
			{Name: "a"},
			{Name: "b"},
		}

		ev := event.UpdateEvent{ObjectOld: oldObj, ObjectNew: newObj}
		if got := p.Update(ev); !got {
			t.Fatalf("Update() = %v, want true", got)
		}
	})
}

func TestCapsuleConfigSpecAdmissionChangedPredicate_Update(t *testing.T) {
	t.Parallel()

	p := predicates.CapsuleConfigSpecAdmissionChangedPredicate{}
	if !p.Create(event.CreateEvent{Object: &capsulev1beta2.CapsuleConfiguration{}}) {
		t.Fatal("configuration creation must initialize admission reconciliation")
	}
	if p.Delete(event.DeleteEvent{Object: &capsulev1beta2.CapsuleConfiguration{}}) {
		t.Fatal("configuration deletion must not enqueue admission reconciliation")
	}
	oldObj := &capsulev1beta2.CapsuleConfiguration{
		Spec: capsulev1beta2.CapsuleConfigurationSpec{
			Admission: capsulev1beta2.DynamicAdmission{
				ServiceName: "capsule-webhook-service",
				Validating:  &capsulev1beta2.DynamicValidatingAdmissionConfig{},
				Mutating:    &capsulev1beta2.DynamicMutatingAdmissionConfig{},
			},
		},
	}

	statusOnly := oldObj.DeepCopy()
	statusOnly.Status.Tenants = []string{"tenant-a"}
	if p.Update(event.UpdateEvent{ObjectOld: oldObj, ObjectNew: statusOnly}) {
		t.Fatal("status-only update with deep-copied admission pointers must be filtered")
	}

	changed := oldObj.DeepCopy()
	changed.Spec.Admission.ServiceName = "replacement-webhook-service"
	if !p.Update(event.UpdateEvent{ObjectOld: oldObj, ObjectNew: changed}) {
		t.Fatal("admission specification change must be admitted")
	}
}
