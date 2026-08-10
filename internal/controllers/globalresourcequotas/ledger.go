// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package globalresourcequotas

import (
	"context"
	"reflect"
	"slices"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/runtime/configuration"
	runtimequota "github.com/projectcapsule/capsule/pkg/runtime/quota"
)

func (r *Controller) ensureLedger(
	ctx context.Context,
	quota *capsulev1beta2.GlobalResourceQuota,
) (*capsulev1beta2.QuantityLedger, error) {
	ledger := &capsulev1beta2.QuantityLedger{
		ObjectMeta: metav1.ObjectMeta{
			Name:      quota.GetLedgerName(),
			Namespace: configuration.ControllerNamespace(),
		},
	}

	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, ledger, func() error {
		ledgerLabels := ledger.GetLabels()
		if ledgerLabels == nil {
			ledgerLabels = map[string]string{}
		}

		ledgerLabels[meta.NewManagedByCapsuleLabel] = meta.ValueController
		ledgerLabels[meta.GlobalResourceQuotaLabel] = quota.Name
		ledger.SetLabels(ledgerLabels)
		ledger.Spec.TargetRef = capsulev1beta2.QuantityLedgerTargetRef{
			APIGroup: capsulev1beta2.GroupVersion.Group,
			Kind:     "GlobalResourceQuota",
			Name:     quota.Name,
			UID:      quota.UID,
		}

		return controllerutil.SetControllerReference(quota, ledger, r.Scheme())
	})
	if err != nil {
		return nil, err
	}

	return ledger, nil
}

func (r *Controller) reconcileLedger(
	ctx context.Context,
	ledger *capsulev1beta2.QuantityLedger,
	generation int64,
	namespaces []string,
	used corev1.ResourceList,
	initialized bool,
) error {
	return retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		current := &capsulev1beta2.QuantityLedger{}
		if err := r.reader.Get(ctx, types.NamespacedName{
			Namespace: ledger.Namespace,
			Name:      ledger.Name,
		}, current); err != nil {
			return err
		}

		before := current.Status.ResourceQuota.DeepCopy()

		next := reconcileLedgerStatus(
			current.Status.ResourceQuota,
			generation,
			namespaces,
			used,
			initialized,
			metav1.Now(),
		)
		if reflect.DeepEqual(before, next) {
			return nil
		}

		current.Status.ResourceQuota = next

		return r.Status().Update(ctx, current)
	})
}

func reconcileLedgerStatus(
	current *capsulev1beta2.QuantityLedgerResourceQuotaStatus,
	generation int64,
	namespaces []string,
	used corev1.ResourceList,
	initialized bool,
	now metav1.Time,
) *capsulev1beta2.QuantityLedgerResourceQuotaStatus {
	if current == nil {
		current = &capsulev1beta2.QuantityLedgerResourceQuotaStatus{}
	} else {
		current = current.DeepCopy()
	}

	increase := positiveDifference(used, current.Used)
	active := make([]capsulev1beta2.QuantityLedgerResourceQuotaReservation, 0, len(current.Reservations))

	for _, reservation := range current.Reservations {
		if reservation.ExpiresAt != nil && reservation.ExpiresAt.Before(&now) {
			continue
		}

		reservation.Delta = consumeResourceList(reservation.Delta, increase)
		if resourceListPositive(reservation.Delta) {
			active = append(active, reservation)
		}
	}

	reserved := capsulev1beta2.ZeroResourceList(used)
	for _, reservation := range active {
		addResourceList(reserved, reservation.Delta)
	}

	allocated := used.DeepCopy()
	addResourceList(allocated, reserved)

	current.ObservedGeneration = generation
	current.Initialized = initialized
	current.Namespaces = slices.Clone(namespaces)
	current.Used = used.DeepCopy()
	current.Reserved = reserved
	current.Allocated = allocated
	current.Reservations = active

	return current
}

func positiveDifference(next, previous corev1.ResourceList) corev1.ResourceList {
	out := make(corev1.ResourceList, len(next))

	for name, quantity := range next {
		delta := quantity.DeepCopy()
		delta.Sub(previous[name])
		runtimequota.ClampQuantityToZero(&delta)
		out[name] = delta
	}

	return out
}

func consumeResourceList(delta, available corev1.ResourceList) corev1.ResourceList {
	out := delta.DeepCopy()
	for name, quantity := range out {
		increase := available[name]
		if quantity.Sign() <= 0 || increase.Sign() <= 0 {
			continue
		}

		consumed := quantity.DeepCopy()
		if consumed.Cmp(increase) > 0 {
			consumed = increase.DeepCopy()
		}

		quantity.Sub(consumed)
		out[name] = quantity

		increase.Sub(consumed)
		available[name] = increase
	}

	return out
}

func addResourceList(target, addition corev1.ResourceList) {
	for name, quantity := range addition {
		current := target[name]
		current.Add(quantity)
		target[name] = current
	}
}

func resourceListPositive(resources corev1.ResourceList) bool {
	for _, quantity := range resources {
		if quantity.Sign() > 0 {
			return true
		}
	}

	return false
}
