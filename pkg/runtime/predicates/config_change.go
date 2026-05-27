// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package predicates

import (
	"sigs.k8s.io/controller-runtime/pkg/event"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
)

type CapsuleConfigSpecAdministratorsChangedPredicate struct{}

func (CapsuleConfigSpecAdministratorsChangedPredicate) Create(event.CreateEvent) bool   { return false }
func (CapsuleConfigSpecAdministratorsChangedPredicate) Delete(event.DeleteEvent) bool   { return false }
func (CapsuleConfigSpecAdministratorsChangedPredicate) Generic(event.GenericEvent) bool { return false }

func (CapsuleConfigSpecAdministratorsChangedPredicate) Update(e event.UpdateEvent) bool {
	oldObj, ok1 := e.ObjectOld.(*capsulev1beta2.CapsuleConfiguration)
	newObj, ok2 := e.ObjectNew.(*capsulev1beta2.CapsuleConfiguration)

	if !ok1 || !ok2 {
		return false
	}

	return len(oldObj.Spec.Administrators) != len(newObj.Spec.Administrators)
}

type CapsuleConfigSpecImpersonationChangedPredicate struct{}

func (CapsuleConfigSpecImpersonationChangedPredicate) Create(event.CreateEvent) bool   { return false }
func (CapsuleConfigSpecImpersonationChangedPredicate) Delete(event.DeleteEvent) bool   { return false }
func (CapsuleConfigSpecImpersonationChangedPredicate) Generic(event.GenericEvent) bool { return false }

func (CapsuleConfigSpecImpersonationChangedPredicate) Update(e event.UpdateEvent) bool {
	oldCfg, ok1 := e.ObjectOld.(*capsulev1beta2.CapsuleConfiguration)
	newCfg, ok2 := e.ObjectNew.(*capsulev1beta2.CapsuleConfiguration)

	if !ok1 || !ok2 {
		return false
	}

	oldSpec := oldCfg.Spec
	newSpec := newCfg.Spec

	return oldSpec.Impersonation != newSpec.Impersonation
}

type CapsuleConfigSpecAdmissionChangedPredicate struct{}

func (CapsuleConfigSpecAdmissionChangedPredicate) Create(event.CreateEvent) bool   { return false }
func (CapsuleConfigSpecAdmissionChangedPredicate) Delete(event.DeleteEvent) bool   { return false }
func (CapsuleConfigSpecAdmissionChangedPredicate) Generic(event.GenericEvent) bool { return false }

func (CapsuleConfigSpecAdmissionChangedPredicate) Update(e event.UpdateEvent) bool {
	oldCfg, ok1 := e.ObjectOld.(*capsulev1beta2.CapsuleConfiguration)
	newCfg, ok2 := e.ObjectNew.(*capsulev1beta2.CapsuleConfiguration)

	if !ok1 || !ok2 {
		return false
	}

	oldSpec := oldCfg.Spec
	newSpec := newCfg.Spec

	return oldSpec.Admission != newSpec.Admission
}
