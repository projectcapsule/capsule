// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package predicates

import (
	"sigs.k8s.io/controller-runtime/pkg/event"
)

type LabelPresentPredicate struct {
	Label string
}

func (p LabelPresentPredicate) Create(e event.CreateEvent) bool {
	if e.Object == nil {
		return false
	}

	_, ok := e.Object.GetLabels()[p.Label]

	return ok
}
func (p LabelPresentPredicate) Delete(e event.DeleteEvent) bool {
	if e.Object == nil {
		return false
	}

	_, ok := e.Object.GetLabels()[p.Label]

	return ok
}

func (p LabelPresentPredicate) Update(e event.UpdateEvent) bool {
	if e.ObjectOld == nil || e.ObjectNew == nil {
		return false
	}

	oldVal := e.ObjectOld.GetLabels()[p.Label]
	newVal := e.ObjectNew.GetLabels()[p.Label]

	return oldVal != newVal
}

func (p LabelPresentPredicate) Generic(event.GenericEvent) bool { return false }
