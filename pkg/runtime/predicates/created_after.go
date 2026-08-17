// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package predicates

import (
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/event"
)

// CreatedAfterPredicate passes the creation of the objects which came to life starting from
// the given time, discarding every other event.
//
// Filtering the event type alone is not enough to react to the new objects only: the
// informer replays its initial list as creation events at every start, thus the already
// existing objects would be processed again on each restart. Comparing the creation
// timestamp discards that replay.
//
// The creation timestamp is tracked by the API Server with a second granularity: Since
// is expected to be truncated accordingly, as NewCreatedAfterPredicate does, otherwise
// the objects created within the very same second would be discarded.
type CreatedAfterPredicate struct {
	Since metav1.Time
}

// NewCreatedAfterPredicate builds a predicate passing the objects created starting from the given time.
func NewCreatedAfterPredicate(since time.Time) CreatedAfterPredicate {
	return CreatedAfterPredicate{
		Since: metav1.NewTime(since.Truncate(time.Second)),
	}
}

func (p CreatedAfterPredicate) Create(e event.CreateEvent) bool {
	if e.Object == nil {
		return false
	}

	created := e.Object.GetCreationTimestamp()

	return !created.Before(&p.Since)
}

func (p CreatedAfterPredicate) Delete(event.DeleteEvent) bool { return false }

func (p CreatedAfterPredicate) Update(event.UpdateEvent) bool { return false }

func (p CreatedAfterPredicate) Generic(event.GenericEvent) bool { return false }
