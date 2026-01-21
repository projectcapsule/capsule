// Copyright 2020-2025 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package predicates

import "sigs.k8s.io/controller-runtime/pkg/event"

type UpdatedLabelsPredicate struct{}

func (UpdatedLabelsPredicate) Create(event.CreateEvent) bool   { return true }
func (UpdatedLabelsPredicate) Delete(event.DeleteEvent) bool   { return true }
func (UpdatedLabelsPredicate) Generic(event.GenericEvent) bool { return false }

func (UpdatedLabelsPredicate) Update(e event.UpdateEvent) bool {
	if e.ObjectOld == nil || e.ObjectNew == nil {
		return false
	}

	return !LabelsEqual(e.ObjectOld.GetLabels(), e.ObjectNew.GetLabels())
}
