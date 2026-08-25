// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package predicates

import (
	"sigs.k8s.io/controller-runtime/pkg/event"

	"github.com/projectcapsule/capsule/pkg/utils"
)

// ObjectMetadataChangedPredicate admits object creation and relevant metadata
// drift, while filtering deletion and status/spec-only updates.
type ObjectMetadataChangedPredicate struct{}

func (ObjectMetadataChangedPredicate) Create(event.CreateEvent) bool   { return true }
func (ObjectMetadataChangedPredicate) Delete(event.DeleteEvent) bool   { return false }
func (ObjectMetadataChangedPredicate) Generic(event.GenericEvent) bool { return false }

func (ObjectMetadataChangedPredicate) Update(e event.UpdateEvent) bool {
	if e.ObjectOld == nil || e.ObjectNew == nil {
		return false
	}

	return !utils.MapEqual(e.ObjectOld.GetLabels(), e.ObjectNew.GetLabels()) ||
		!utils.MapEqual(e.ObjectOld.GetAnnotations(), e.ObjectNew.GetAnnotations())
}
