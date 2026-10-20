// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package tenant

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"

	apivalidation "k8s.io/apimachinery/pkg/api/validation"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
)

// LegacyQuotaReservationsAnnotation shares the usage counter's protected prefix.
// Reservations and usage are written atomically on the same Tenant, including
// when webhook and controller requests are served by different replicas.
const LegacyQuotaReservationsAnnotation = capsulev1beta2.ResourceUsedAnnotationPrefix + "/reservations"

var ErrLegacyQuotaStateCapacity = errors.New("legacy quota coordination state capacity exceeded")

// LegacyQuotaReservation uses the existing ledger reservation representation.
// NamespaceUID additionally prevents a recreated namespace materializing an old
// reservation. This is internal coordination for the legacy annotation API.
type LegacyQuotaReservation struct {
	capsulev1beta2.QuantityLedgerReservation

	NamespaceUID types.UID `json:"namespaceUid"`
}

// LegacyQuotaDeletion schedules authoritative recounts until deletion is
// observed. Admission alone cannot prove that a DELETE will be persisted.
type LegacyQuotaDeletion struct {
	capsulev1beta2.QuantityLedgerPendingDelete

	NamespaceUID types.UID    `json:"namespaceUid"`
	ExpiresAt    *metav1.Time `json:"expiresAt"`
}

type LegacyQuotaReservations struct {
	Pending  map[string][]LegacyQuotaReservation `json:"pending,omitempty"`
	Deleting map[string][]LegacyQuotaDeletion    `json:"deleting,omitempty"`
	// Namespace pins bridge informer/status visibility independently of create TTL.
	Namespaces map[string]types.UID `json:"namespaces,omitempty"`
}

func ReadLegacyQuotaReservations(annotations map[string]string) (*LegacyQuotaReservations, error) {
	reservations := &LegacyQuotaReservations{Pending: make(map[string][]LegacyQuotaReservation), Namespaces: make(map[string]types.UID)}
	if value := annotations[LegacyQuotaReservationsAnnotation]; value != "" {
		if err := json.Unmarshal([]byte(value), &reservations); err != nil {
			return nil, fmt.Errorf("decode legacy quota reservations: %w", err)
		}

		if reservations == nil {
			return nil, fmt.Errorf("legacy quota reservations must be an object")
		}
	}

	if reservations.Pending == nil {
		reservations.Pending = make(map[string][]LegacyQuotaReservation)
	}

	if reservations.Namespaces == nil {
		reservations.Namespaces = make(map[string]types.UID)
	}

	for name, uid := range reservations.Namespaces {
		if name == "" || uid == "" {
			return nil, fmt.Errorf("legacy quota namespace pin requires a name and UID")
		}
	}

	for _, entries := range reservations.Pending {
		for _, entry := range entries {
			if entry.ID == "" || entry.NamespaceUID == "" || entry.ObjectRef.Namespace == "" || entry.ExpiresAt == nil {
				return nil, fmt.Errorf("legacy quota reservation has incomplete identity or expiry")
			}
		}
	}

	for _, entries := range reservations.Deleting {
		for _, entry := range entries {
			if entry.ID == "" || entry.NamespaceUID == "" || entry.ObjectRef.Namespace == "" || entry.ObjectRef.Name == "" || entry.ExpiresAt == nil {
				return nil, fmt.Errorf("legacy quota deletion has incomplete identity or expiry")
			}
		}
	}

	return reservations, nil
}

func WriteLegacyQuotaReservations(annotations map[string]string, reservations *LegacyQuotaReservations) error {
	count := 0

	for key, entries := range reservations.Pending {
		if len(entries) == 0 {
			delete(reservations.Pending, key)
		}

		count += len(entries)
	}

	for key, entries := range reservations.Deleting {
		if len(entries) == 0 {
			delete(reservations.Deleting, key)
		}

		count += len(entries)
	}

	if count == 0 && len(reservations.Namespaces) == 0 {
		return writeLegacyQuotaState(annotations, "")
	}
	// Bound admission work and leave room for the Tenant's existing annotations.
	if count > 1024 {
		return fmt.Errorf("%w: too many pending legacy quota reservations; retry after reconciliation", ErrLegacyQuotaStateCapacity)
	}

	value, err := json.Marshal(reservations)
	if err != nil {
		return fmt.Errorf("encode legacy quota reservations: %w", err)
	}

	if len(value) > 192*1024 {
		return fmt.Errorf("%w: legacy quota reservations exceed metadata budget; retry after reconciliation", ErrLegacyQuotaStateCapacity)
	}

	return writeLegacyQuotaState(annotations, string(value))
}

func writeLegacyQuotaState(annotations map[string]string, value string) error {
	previous, existed := annotations[LegacyQuotaReservationsAnnotation]

	if value == "" {
		delete(annotations, LegacyQuotaReservationsAnnotation)
	} else {
		annotations[LegacyQuotaReservationsAnnotation] = value
	}

	// Kubernetes counts every annotation key and value. Callers prepare usage
	// updates before this write, so validation includes the complete final map.
	if err := apivalidation.ValidateAnnotationsSize(annotations); err != nil {
		if existed {
			annotations[LegacyQuotaReservationsAnnotation] = previous
		} else {
			delete(annotations, LegacyQuotaReservationsAnnotation)
		}

		return fmt.Errorf("%w: total Tenant annotations exceed metadata budget: %w", ErrLegacyQuotaStateCapacity, err)
	}

	return nil
}

// LegacyQuotaRequeueAfter only schedules the Tenant/resource-quota queues. It
// never holds the independent namespace permission queue while objects persist.
func LegacyQuotaRequeueAfter(annotations map[string]string, now time.Time) time.Duration {
	if annotations[LegacyQuotaReservationsAnnotation] == "" {
		return 0
	}

	reservations, err := ReadLegacyQuotaReservations(annotations)
	if err != nil {
		return 2 * time.Second
	}

	var delay time.Duration
	if len(reservations.Namespaces) > 0 {
		delay = 2 * time.Second
	}

	for _, entries := range reservations.Pending {
		for _, entry := range entries {
			candidate := legacyQuotaRetryDelay(now, entry.CreatedAt, entry.ExpiresAt)
			if delay == 0 || candidate < delay {
				delay = candidate
			}
		}
	}

	for _, entries := range reservations.Deleting {
		for _, entry := range entries {
			candidate := legacyQuotaRetryDelay(now, entry.CreatedAt, entry.ExpiresAt)
			if delay == 0 || candidate < delay {
				delay = candidate
			}
		}
	}

	return delay
}

func legacyQuotaRetryDelay(now time.Time, created metav1.Time, expires *metav1.Time) time.Duration {
	delay := 2 * time.Second
	if age := now.Sub(created.Time); age >= 30*time.Second {
		delay = 15 * time.Second
	} else if age >= 5*time.Second {
		delay = 5 * time.Second
	}

	if expires != nil {
		if remaining := expires.Sub(now); remaining <= 0 {
			return time.Millisecond
		} else if remaining < delay {
			delay = remaining
		}
	}

	return delay
}
