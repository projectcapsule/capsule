// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package customquota

import (
	"context"
	"fmt"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/runtime/configuration"
	"github.com/projectcapsule/capsule/pkg/runtime/quota"
)

const (
	maxQuantityLedgerReservations   = 1024
	maxQuantityLedgerPendingDeletes = 1024
)

func quantityLedgerKeyForMatchedQuota(item evaluatedQuota) types.NamespacedName {
	if item.IsGlobal {
		return types.NamespacedName{
			Name:      item.Name,
			Namespace: configuration.ControllerNamespace(),
		}
	}

	return types.NamespacedName{
		Name:      item.Name,
		Namespace: item.Namespace,
	}
}

func reserveCreateOnLedger(
	ctx context.Context,
	c client.Client,
	reader client.Reader,
	item evaluatedQuota,
	reservation *capsulev1beta2.QuantityLedgerReservation,
	dryRun bool,
) (bool, resource.Quantity, resource.Quantity, error) {
	var (
		allowed       bool
		effectiveUsed resource.Quantity
		reserved      resource.Quantity
	)

	ledgerKey := quantityLedgerKeyForMatchedQuota(item)

	err := retry.RetryOnConflict(ledgerMutationBackoff, func() error {
		ledger := &capsulev1beta2.QuantityLedger{}
		if err := reader.Get(ctx, ledgerKey, ledger); err != nil {
			return err
		}

		now := metav1.Now()

		// Idempotency: if this admission request already has a reservation,
		// do not increment Allocated a second time.
		activeReservations := make([]capsulev1beta2.QuantityLedgerReservation, 0, len(ledger.Status.Reservations)+1)
		foundReservation := false

		for _, existing := range ledger.Status.Reservations {
			if existing.ExpiresAt != nil && existing.ExpiresAt.Before(&now) {
				continue
			}

			if existing.ID == reservation.ID {
				foundReservation = true

				// Keep Allocated unchanged for retry/idempotent update.
				existing.Usage = reservation.Usage.DeepCopy()
				existing.Delta = copyQuantityPtr(reservation.Delta)
				existing.ObjectRef = reservation.ObjectRef
				existing.UpdatedAt = now
				existing.ExpiresAt = reservation.ExpiresAt
			}

			activeReservations = append(activeReservations, existing)
		}

		if !foundReservation {
			if len(activeReservations) >= maxQuantityLedgerReservations {
				return fmt.Errorf(
					"quantity ledger %s has reached the maximum of %d inflight reservations",
					ledgerKey.String(),
					maxQuantityLedgerReservations,
				)
			}

			activeReservations = append(activeReservations, *reservation)
		}

		newReserved := sumReservationDeltas(activeReservations)
		nextAllocated := observedLedgerAllocation(ledger)
		nextAllocated.Add(newReserved)

		if nextAllocated.Cmp(item.Limit) > 0 {
			allowed = false
			effectiveUsed = nextAllocated
			reserved = ledger.Status.Reserved.DeepCopy()

			return nil
		}

		if dryRun {
			allowed = true
			effectiveUsed = nextAllocated
			reserved = ledger.Status.Reserved.DeepCopy()

			return nil
		}

		ledger.Status.Reservations = activeReservations
		ledger.Status.Reserved = newReserved
		ledger.Status.Allocated = nextAllocated

		if err := c.Status().Update(ctx, ledger); err != nil {
			return err
		}

		allowed = true
		effectiveUsed = nextAllocated
		reserved = newReserved

		return nil
	})

	return allowed, effectiveUsed, reserved, err
}

func replaceUsageOnLedger(
	ctx context.Context,
	c client.Client,
	reader client.Reader,
	item evaluatedQuota,
	_ resource.Quantity,
	_ resource.Quantity,
	reservation *capsulev1beta2.QuantityLedgerReservation,
	pendingDelete *capsulev1beta2.QuantityLedgerPendingDelete,
	enforceLimit bool,
	dryRun bool,
) (bool, resource.Quantity, resource.Quantity, error) {
	var (
		allowed       bool
		effectiveUsed resource.Quantity
		reserved      resource.Quantity
	)

	ledgerKey := quantityLedgerKeyForMatchedQuota(item)

	err := retry.RetryOnConflict(ledgerMutationBackoff, func() error {
		ledger := &capsulev1beta2.QuantityLedger{}
		if err := reader.Get(ctx, ledgerKey, ledger); err != nil {
			return err
		}

		now := metav1.Now()

		activeReservations := make([]capsulev1beta2.QuantityLedgerReservation, 0, len(ledger.Status.Reservations)+1)
		foundReservation := false

		for _, existing := range ledger.Status.Reservations {
			if existing.ExpiresAt != nil && existing.ExpiresAt.Before(&now) {
				continue
			}

			if reservation != nil && existing.ID == reservation.ID {
				foundReservation = true
				existing.Usage = reservation.Usage.DeepCopy()
				existing.Delta = copyQuantityPtr(reservation.Delta)
				existing.ObjectRef = reservation.ObjectRef
				existing.UpdatedAt = now
				existing.ExpiresAt = reservation.ExpiresAt
			}

			activeReservations = append(activeReservations, existing)
		}

		if reservation != nil && !foundReservation {
			if len(activeReservations) >= maxQuantityLedgerReservations {
				return fmt.Errorf(
					"quantity ledger %s has reached the maximum of %d inflight reservations",
					ledgerKey.String(),
					maxQuantityLedgerReservations,
				)
			}

			activeReservations = append(activeReservations, *reservation)
		}

		activeDeletes := make([]capsulev1beta2.QuantityLedgerPendingDelete, 0, len(ledger.Status.PendingDeletes)+1)
		activeDeletes = append(activeDeletes, ledger.Status.PendingDeletes...)

		if pendingDelete != nil {
			exists := false

			for _, pd := range activeDeletes {
				if sameQuantityLedgerPendingDelete(pd, *pendingDelete) {
					exists = true

					break
				}
			}

			if !exists {
				if len(activeDeletes) >= maxQuantityLedgerPendingDeletes {
					return fmt.Errorf(
						"quantity ledger %s has reached the maximum of %d pending deletes",
						ledgerKey.String(),
						maxQuantityLedgerPendingDeletes,
					)
				}

				newPendingDelete := *pendingDelete
				newPendingDelete.CreatedAt = now
				activeDeletes = append(activeDeletes, newPendingDelete)
			}
		}

		// Never release capacity from admission. The API operation can still
		// fail after this webhook returns, so only the positive usage delta is
		// reserved here. Reconciliation releases decreases after observing the
		// persisted object.
		newReserved := sumReservationDeltas(activeReservations)
		nextAllocated := observedLedgerAllocation(ledger)
		nextAllocated.Add(newReserved)

		if enforceLimit && nextAllocated.Cmp(item.Limit) > 0 {
			allowed = false
			effectiveUsed = nextAllocated
			reserved = ledger.Status.Reserved.DeepCopy()

			return nil
		}

		if dryRun {
			allowed = true
			effectiveUsed = nextAllocated
			reserved = ledger.Status.Reserved.DeepCopy()

			return nil
		}

		ledger.Status.Reservations = activeReservations
		ledger.Status.PendingDeletes = activeDeletes
		ledger.Status.Reserved = newReserved
		ledger.Status.Allocated = nextAllocated

		if err := c.Status().Update(ctx, ledger); err != nil {
			return err
		}

		allowed = true
		effectiveUsed = nextAllocated
		reserved = newReserved

		return nil
	})

	return allowed, effectiveUsed, reserved, err
}

func rollbackUsageReplacementOnLedger(
	ctx context.Context,
	c client.Client,
	reader client.Reader,
	ledgerKey types.NamespacedName,
	reservationID string,
	_ resource.Quantity,
	_ resource.Quantity,
	pendingDelete *capsulev1beta2.QuantityLedgerPendingDelete,
) error {
	return retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		ledger := &capsulev1beta2.QuantityLedger{}
		if err := reader.Get(ctx, ledgerKey, ledger); err != nil {
			if apierrors.IsNotFound(err) {
				return nil
			}

			return err
		}

		activeReservations := make([]capsulev1beta2.QuantityLedgerReservation, 0, len(ledger.Status.Reservations))
		released := resource.MustParse("0")

		for _, res := range ledger.Status.Reservations {
			if reservationID != "" && res.ID == reservationID {
				released.Add(reservationDelta(res))

				continue
			}

			activeReservations = append(activeReservations, res)
		}

		activeDeletes := make([]capsulev1beta2.QuantityLedgerPendingDelete, 0, len(ledger.Status.PendingDeletes))
		removedPendingDelete := false

		for _, pd := range ledger.Status.PendingDeletes {
			if pendingDelete != nil && sameQuantityLedgerPendingDelete(pd, *pendingDelete) {
				removedPendingDelete = true

				continue
			}

			activeDeletes = append(activeDeletes, pd)
		}

		if released.Sign() == 0 && !removedPendingDelete {
			return nil
		}

		allocated := ledger.Status.Allocated.DeepCopy()
		if allocated.IsZero() {
			allocated = resource.MustParse("0")
		}

		allocated.Sub(released)
		quota.ClampQuantityToZero(&allocated)

		newReserved := resource.MustParse("0")
		for _, res := range activeReservations {
			newReserved.Add(reservationDelta(res))
		}

		ledger.Status.Allocated = allocated
		ledger.Status.Reservations = activeReservations
		ledger.Status.PendingDeletes = activeDeletes
		ledger.Status.Reserved = newReserved

		return c.Status().Update(ctx, ledger)
	})
}

func sameQuantityLedgerPendingDelete(
	a capsulev1beta2.QuantityLedgerPendingDelete,
	b capsulev1beta2.QuantityLedgerPendingDelete,
) bool {
	if a.ID != "" || b.ID != "" {
		return a.ID != "" && a.ID == b.ID
	}

	if a.ObjectRef.UID != "" && b.ObjectRef.UID != "" {
		return a.ObjectRef.UID == b.ObjectRef.UID
	}

	return a.ObjectRef.APIGroup == b.ObjectRef.APIGroup &&
		a.ObjectRef.APIVersion == b.ObjectRef.APIVersion &&
		a.ObjectRef.Kind == b.ObjectRef.Kind &&
		a.ObjectRef.Namespace == b.ObjectRef.Namespace &&
		a.ObjectRef.Name == b.ObjectRef.Name
}

func buildReservation(
	req admission.Request,
	u unstructured.Unstructured,
	usage resource.Quantity,
	delta resource.Quantity,
	quotaKey string,
) capsulev1beta2.QuantityLedgerReservation {
	now := metav1.Now()
	expiresAt := metav1.NewTime(now.Add(2 * time.Minute))

	return capsulev1beta2.QuantityLedgerReservation{
		ID: fmt.Sprintf("%s/%s", req.UID, quotaKey),
		ObjectRef: capsulev1beta2.QuantityLedgerObjectRef{
			APIGroup:   req.Kind.Group,
			APIVersion: req.Kind.Version,
			Kind:       req.Kind.Kind,
			Namespace:  u.GetNamespace(),
			Name:       u.GetName(),
			UID:        u.GetUID(),
		},
		Usage:     usage.DeepCopy(),
		Delta:     quantityPtr(delta),
		CreatedAt: now,
		UpdatedAt: now,
		ExpiresAt: &expiresAt,
	}
}

func quantityPtr(in resource.Quantity) *resource.Quantity {
	out := in.DeepCopy()

	return &out
}

func copyQuantityPtr(in *resource.Quantity) *resource.Quantity {
	if in == nil {
		return nil
	}

	return quantityPtr(*in)
}

func reservationDelta(res capsulev1beta2.QuantityLedgerReservation) resource.Quantity {
	if res.Delta == nil {
		return res.Usage.DeepCopy()
	}

	return res.Delta.DeepCopy()
}

func observedLedgerAllocation(ledger *capsulev1beta2.QuantityLedger) resource.Quantity {
	observed := ledger.Status.Allocated.DeepCopy()
	observed.Sub(ledger.Status.Reserved)
	quota.ClampQuantityToZero(&observed)

	return observed
}

func sumReservationDeltas(
	reservations []capsulev1beta2.QuantityLedgerReservation,
) resource.Quantity {
	total := resource.MustParse("0")

	for _, reservation := range reservations {
		total.Add(reservationDelta(reservation))
	}

	return total
}

func allKeys[K comparable, V any](a map[K]V, b map[K]V) []K {
	out := make([]K, 0, len(a)+len(b))
	seen := make(map[K]struct{}, len(a)+len(b))

	for k := range a {
		seen[k] = struct{}{}

		out = append(out, k)
	}

	for k := range b {
		if _, ok := seen[k]; ok {
			continue
		}

		out = append(out, k)
	}

	return out
}

func sourcesChanged(a, b []capsulev1beta2.CustomQuotaSpecSource) bool {
	if len(a) != len(b) {
		return true
	}

	for i := range a {
		if a[i].APIVersion != b[i].APIVersion ||
			a[i].Kind != b[i].Kind ||
			a[i].Path != b[i].Path ||
			a[i].Operation != b[i].Operation {
			return true
		}
	}

	return false
}
