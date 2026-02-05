// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package predicates

import (
	"sigs.k8s.io/controller-runtime/pkg/event"

	"github.com/projectcapsule/capsule/pkg/utils"
)

type UpdatedMetadataPredicate struct{}

func (UpdatedMetadataPredicate) Create(event.CreateEvent) bool   { return true }
func (UpdatedMetadataPredicate) Delete(event.DeleteEvent) bool   { return true }
func (UpdatedMetadataPredicate) Generic(event.GenericEvent) bool { return false }

func (UpdatedMetadataPredicate) Update(e event.UpdateEvent) bool {
	if e.ObjectOld == nil || e.ObjectNew == nil {
		return false
	}

	if !utils.MapEqual(e.ObjectOld.GetLabels(), e.ObjectNew.GetLabels()) {
		return true
	}

	return !utils.MapEqual(e.ObjectOld.GetAnnotations(), e.ObjectNew.GetAnnotations())
}
