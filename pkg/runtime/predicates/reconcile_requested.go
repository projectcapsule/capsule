// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package predicates

import (
	"sigs.k8s.io/controller-runtime/pkg/event"

	"github.com/projectcapsule/capsule/pkg/api/meta"
)

// Only Trigger a Reconcile when the requested annotation has changed value or was added
type ReconcileRequestedPredicate struct{}

func (ReconcileRequestedPredicate) Create(e event.CreateEvent) bool   { return false }
func (ReconcileRequestedPredicate) Delete(e event.DeleteEvent) bool   { return false }
func (ReconcileRequestedPredicate) Generic(e event.GenericEvent) bool { return false }

func (ReconcileRequestedPredicate) Update(e event.UpdateEvent) bool {
	if e.ObjectOld == nil || e.ObjectNew == nil {
		return false
	}

	oldA := e.ObjectOld.GetAnnotations()
	newA := e.ObjectNew.GetAnnotations()

	oldV := ""
	if oldA != nil {
		oldV = oldA[meta.ReconcileAnnotation]
	}
	newV := ""
	if newA != nil {
		newV = newA[meta.ReconcileAnnotation]
	}

	return newV != "" && newV != oldV
}
