// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package v1beta2

import (
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// QuantityLedgerReservation represents one active inflight reservation.
// ID should be stable for retries of the same admission request.
// In practice, admission.Request.UID is a good default.
type QuantityLedgerReservation struct {
	// Unique reservation identifier.
	// +kubebuilder:validation:MinLength=1
	ID string `json:"id"`

	// Amount reserved for this request.
	Usage resource.Quantity `json:"usage"`

	// Object that this reservation is intended to create/update.
	ObjectRef QuantityLedgerObjectRef `json:"objectRef"`

	// Time the reservation was first created.
	CreatedAt metav1.Time `json:"createdAt"`

	// Time the reservation was last refreshed or updated.
	UpdatedAt metav1.Time `json:"updatedAt"`

	// Time after which the reservation may be considered stale.
	// +optional
	ExpiresAt *metav1.Time `json:"expiresAt,omitempty"`
}

// QuantityLedgerPendingDelete tracks objects that are expected to disappear from claims
// soon, but may still temporarily appear during rebuild due to propagation delay.
type QuantityLedgerPendingDelete struct {
	ObjectRef QuantityLedgerObjectRef `json:"objectRef"`
	CreatedAt metav1.Time             `json:"createdAt"`
}

// QuantityLedgerStatus contains the mutable coordination state used by admission
// and quota controllers.
type QuantityLedgerStatus struct {
	// Reserved is the aggregate sum of all active reservations.
	// Controllers/webhooks should treat this as derived data from Reservations.
	// +optional
	Reserved resource.Quantity `json:"reserved,omitempty"`

	// Active inflight reservations for this quota.
	// +optional
	Reservations []QuantityLedgerReservation `json:"reservations,omitempty"`

	// Pending delete hints carried over from admission delete handling.
	// +optional
	PendingDeletes []QuantityLedgerPendingDelete `json:"pendingDeletes,omitempty"`

	// Conditions for the resource claim
	// +optional
	Conditions meta.ConditionList `json:"conditions,omitzero"`
}
