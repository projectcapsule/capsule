// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package tenant

import (
	"encoding/json"
	"fmt"
	"time"

	"k8s.io/apimachinery/pkg/types"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
)

// LegacyQuotaReservationsAnnotation shares the usage counter's protected prefix.
// Reservations and usage are written atomically on the same Tenant, including
// when webhook and controller requests are served by different replicas.
const LegacyQuotaReservationsAnnotation = capsulev1beta2.ResourceUsedAnnotationPrefix + "/reservations"

// LegacyQuotaReservation uses the existing ledger reservation representation.
// NamespaceUID additionally prevents a recreated namespace materializing an old
// reservation. This is internal coordination for the legacy annotation API.
type LegacyQuotaReservation struct {
	capsulev1beta2.QuantityLedgerReservation

	NamespaceUID types.UID `json:"namespaceUid"`
}

type LegacyQuotaReservations struct {
	Pending map[string][]LegacyQuotaReservation `json:"pending,omitempty"`
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

	if count == 0 && len(reservations.Namespaces) == 0 {
		delete(annotations, LegacyQuotaReservationsAnnotation)

		return nil
	}
	// Bound admission work and leave room for the Tenant's existing annotations.
	if count > 1024 {
		return fmt.Errorf("too many pending legacy quota reservations; retry after reconciliation")
	}

	value, err := json.Marshal(reservations)
	if err != nil {
		return fmt.Errorf("encode legacy quota reservations: %w", err)
	}

	if len(value) > 192*1024 {
		return fmt.Errorf("legacy quota reservations exceed metadata budget; retry after reconciliation")
	}

	annotations[LegacyQuotaReservationsAnnotation] = string(value)

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
			candidate := 2 * time.Second
			if age := now.Sub(entry.CreatedAt.Time); age >= 30*time.Second {
				candidate = 15 * time.Second
			} else if age >= 5*time.Second {
				candidate = 5 * time.Second
			}

			if entry.ExpiresAt != nil {
				if remaining := entry.ExpiresAt.Sub(now); remaining <= 0 {
					candidate = time.Millisecond
				} else if remaining < candidate {
					candidate = remaining
				}
			}

			if delay == 0 || candidate < delay {
				delay = candidate
			}
		}
	}

	return delay
}
