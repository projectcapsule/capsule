// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package predicates

import (
	"sigs.k8s.io/controller-runtime/pkg/event"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
)

type CapsuleConfigSpecChangedPredicate struct{}

func (CapsuleConfigSpecChangedPredicate) Create(event.CreateEvent) bool   { return false }
func (CapsuleConfigSpecChangedPredicate) Delete(event.DeleteEvent) bool   { return false }
func (CapsuleConfigSpecChangedPredicate) Generic(event.GenericEvent) bool { return false }

func (CapsuleConfigSpecChangedPredicate) Update(e event.UpdateEvent) bool {
	oldObj, ok1 := e.ObjectOld.(*capsulev1beta2.CapsuleConfiguration)
	newObj, ok2 := e.ObjectNew.(*capsulev1beta2.CapsuleConfiguration)

	if !ok1 || !ok2 {
		return false
	}

	return len(oldObj.Spec.Administrators) != len(newObj.Spec.Administrators)
}
