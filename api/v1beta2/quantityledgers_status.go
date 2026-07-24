// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package v1beta2

import (
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/projectcapsule/capsule/pkg/api/meta"
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

	// Delta is the additional amount held against the quota while the admitted
	// object is materializing. For creates this is normally equal to Usage. For
	// updates it is max(newUsage-oldUsage, 0), so admission never releases
	// capacity before the API server has persisted the update.
	//
	// A nil value is interpreted as Usage for backwards compatibility with
	// ledgers written before this field was introduced.
	// +optional
	Delta *resource.Quantity `json:"delta,omitempty"`

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
	// ID identifies the admission request that added this hint. It allows a
	// failed multi-quota admission to roll back only its own hint.
	// +optional
	ID string `json:"id,omitempty"`

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

	// Allocated is the admission-owned total that has been accepted by the webhook.
	// It must be updated only through optimistic concurrency on QuantityLedger.
	Allocated resource.Quantity `json:"allocated,omitempty"`
}
