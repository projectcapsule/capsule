// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package predicates

import "sigs.k8s.io/controller-runtime/pkg/event"

// DeletionChangedPredicate admits lifecycle events and updates that start or
// change deletion. It filters status-only updates while preserving finalizer
// reconciliation when deletion begins.
type DeletionChangedPredicate struct{}

func (DeletionChangedPredicate) Create(event.CreateEvent) bool   { return true }
func (DeletionChangedPredicate) Delete(event.DeleteEvent) bool   { return true }
func (DeletionChangedPredicate) Generic(event.GenericEvent) bool { return false }

func (DeletionChangedPredicate) Update(e event.UpdateEvent) bool {
	if e.ObjectOld == nil || e.ObjectNew == nil {
		return false
	}

	oldTimestamp := e.ObjectOld.GetDeletionTimestamp()
	newTimestamp := e.ObjectNew.GetDeletionTimestamp()

	if oldTimestamp == nil || newTimestamp == nil {
		return oldTimestamp != newTimestamp
	}

	return !oldTimestamp.Equal(newTimestamp)
}
