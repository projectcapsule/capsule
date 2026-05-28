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

		allocated := ledger.Status.Allocated.DeepCopy()
		if allocated.IsZero() {
			allocated = resource.MustParse("0")
		}

		requested := reservation.Usage.DeepCopy()

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
				existing.ObjectRef = reservation.ObjectRef
				existing.UpdatedAt = now
				existing.ExpiresAt = reservation.ExpiresAt
			}

			activeReservations = append(activeReservations, existing)
		}

		nextAllocated := allocated.DeepCopy()
		if !foundReservation {
			nextAllocated.Add(requested)
		}

		if nextAllocated.Cmp(item.Limit) > 0 {
			allowed = false
			effectiveUsed = nextAllocated
			reserved = allocated

			return nil
		}

		if !foundReservation {
			activeReservations = append(activeReservations, *reservation)
		}

		newReserved := resource.MustParse("0")
		for _, r := range activeReservations {
			newReserved.Add(r.Usage)
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
	oldUsage resource.Quantity,
	newUsage resource.Quantity,
	reservation *capsulev1beta2.QuantityLedgerReservation,
	pendingDelete *capsulev1beta2.QuantityLedgerObjectRef,
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
				existing.ObjectRef = reservation.ObjectRef
				existing.UpdatedAt = now
				existing.ExpiresAt = reservation.ExpiresAt
			}

			activeReservations = append(activeReservations, existing)
		}

		if reservation != nil && !foundReservation {
			activeReservations = append(activeReservations, *reservation)
		}

		activeDeletes := make([]capsulev1beta2.QuantityLedgerPendingDelete, 0, len(ledger.Status.PendingDeletes)+1)
		activeDeletes = append(activeDeletes, ledger.Status.PendingDeletes...)

		if pendingDelete != nil {
			exists := false

			for _, pd := range activeDeletes {
				if pd.ObjectRef.UID != "" && pd.ObjectRef.UID == pendingDelete.UID {
					exists = true

					break
				}
			}

			if !exists {
				activeDeletes = append(activeDeletes, capsulev1beta2.QuantityLedgerPendingDelete{
					ObjectRef: *pendingDelete,
					CreatedAt: now,
				})
			}
		}

		nextAllocated := ledger.Status.Allocated.DeepCopy()
		if nextAllocated.IsZero() {
			nextAllocated = resource.MustParse("0")
		}

		nextAllocated.Sub(oldUsage)
		quota.ClampQuantityToZero(&nextAllocated)

		nextAllocated.Add(newUsage)

		if nextAllocated.Cmp(item.Limit) > 0 {
			allowed = false
			effectiveUsed = nextAllocated
			reserved = ledger.Status.Reserved.DeepCopy()

			return nil
		}

		newReserved := resource.MustParse("0")
		for _, res := range activeReservations {
			newReserved.Add(res.Usage)
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
	oldUsage resource.Quantity,
	newUsage resource.Quantity,
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

		for _, res := range ledger.Status.Reservations {
			if reservationID != "" && res.ID == reservationID {
				continue
			}

			activeReservations = append(activeReservations, res)
		}

		allocated := ledger.Status.Allocated.DeepCopy()
		if allocated.IsZero() {
			allocated = resource.MustParse("0")
		}

		allocated.Sub(newUsage)
		quota.ClampQuantityToZero(&allocated)
		allocated.Add(oldUsage)

		newReserved := resource.MustParse("0")
		for _, res := range activeReservations {
			newReserved.Add(res.Usage)
		}

		ledger.Status.Allocated = allocated
		ledger.Status.Reservations = activeReservations
		ledger.Status.Reserved = newReserved

		return c.Status().Update(ctx, ledger)
	})
}

func buildReservation(
	req admission.Request,
	u unstructured.Unstructured,
	usage resource.Quantity,
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
		CreatedAt: now,
		UpdatedAt: now,
		ExpiresAt: &expiresAt,
	}
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
