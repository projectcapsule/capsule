// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package globalresourcequota

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	quotaevaluator "github.com/projectcapsule/capsule/internal/quota/evaluator"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	ad "github.com/projectcapsule/capsule/pkg/runtime/admission"
	"github.com/projectcapsule/capsule/pkg/runtime/configuration"
	"github.com/projectcapsule/capsule/pkg/runtime/events"
	"github.com/projectcapsule/capsule/pkg/runtime/handlers"
	runtimequota "github.com/projectcapsule/capsule/pkg/runtime/quota"
)

const maxReservations = 1024

var ledgerBackoff = wait.Backoff{
	Steps:    8,
	Duration: 10 * time.Millisecond,
	Factor:   1.6,
	Jitter:   0.2,
}

type handler struct{}

type appliedReservation struct {
	Key types.NamespacedName
	ID  string
}

func Handler() handlers.Handler {
	return &handler{}
}

func (h *handler) OnCreate(
	c client.Client,
	reader client.Reader,
	_ admission.Decoder,
	_ events.EventRecorder,
) handlers.Func {
	return h.handle(c, reader)
}

func (h *handler) OnUpdate(
	c client.Client,
	reader client.Reader,
	_ admission.Decoder,
	_ events.EventRecorder,
) handlers.Func {
	return h.handle(c, reader)
}

func (h *handler) OnDelete(
	client.Client,
	client.Reader,
	admission.Decoder,
	events.EventRecorder,
) handlers.Func {
	return func(context.Context, admission.Request) *admission.Response {
		return nil
	}
}

func (h *handler) handle(c client.Client, reader client.Reader) handlers.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		if isGlobalResourceQuotaRequest(req) {
			return validateGlobalResourceQuotaRequest(ctx, reader, req)
		}

		return enforceGlobalResourceQuotaRequest(ctx, c, reader, req)
	}
}

func validateGlobalResourceQuotaRequest(
	ctx context.Context,
	reader client.Reader,
	req admission.Request,
) *admission.Response {
	quota := &capsulev1beta2.GlobalResourceQuota{}
	if err := json.Unmarshal(req.Object.Raw, quota); err != nil {
		return ad.Denyf("GlobalResourceQuota could not be decoded: %v", err)
	}

	if err := validateGlobalResourceQuota(quota); err != nil {
		return ad.Denyf("invalid GlobalResourceQuota: %v", err)
	}

	if req.Operation != admissionv1.Update {
		return nil
	}

	oldQuota := &capsulev1beta2.GlobalResourceQuota{}
	if err := json.Unmarshal(req.OldObject.Raw, oldQuota); err != nil {
		return ad.Denyf("previous GlobalResourceQuota could not be decoded: %v", err)
	}

	if err := validateHardLimit(quota.Spec.Quota.Hard, oldQuota.Status.Total.Used); err != nil {
		return ad.Denyf("invalid GlobalResourceQuota: %v", err)
	}

	ledger := &capsulev1beta2.QuantityLedger{}

	err := reader.Get(ctx, types.NamespacedName{
		Namespace: configuration.ControllerNamespace(),
		Name:      oldQuota.GetLedgerName(),
	}, ledger)

	switch {
	case apierrors.IsNotFound(err):
		return nil
	case err != nil:
		return ad.ErroredResponse(err)
	case ledger.Spec.TargetRef.UID != oldQuota.UID || ledger.Status.ResourceQuota == nil:
		return nil
	}

	if err := validateHardLimit(
		quota.Spec.Quota.Hard,
		ledger.Status.ResourceQuota.Allocated,
	); err != nil {
		return ad.Denyf("invalid GlobalResourceQuota: %v", err)
	}

	return nil
}

func enforceGlobalResourceQuotaRequest(
	ctx context.Context,
	c client.Client,
	reader client.Reader,
	req admission.Request,
) *admission.Response {
	if req.Namespace == "" {
		return nil
	}

	namespace := &corev1.Namespace{}
	if err := c.Get(ctx, client.ObjectKey{Name: req.Namespace}, namespace); err != nil {
		if apierrors.IsNotFound(err) {
			return nil
		}

		return ad.ErroredResponse(err)
	}

	allQuotas := &capsulev1beta2.GlobalResourceQuotaList{}
	if err := c.List(ctx, allQuotas); err != nil {
		return ad.ErroredResponse(err)
	}

	quotaList, err := matchingGlobalResourceQuotas(namespace, allQuotas.Items)
	if err != nil {
		return ad.ErroredResponse(err)
	}

	if len(quotaList) == 0 {
		return nil
	}

	evaluation, handled, err := quotaevaluator.Evaluate(req)
	if err != nil {
		return ad.Denyf("GlobalResourceQuota usage could not be calculated: %v", err)
	}

	if !handled || isManagedResourceQuota(req, evaluation.New) {
		// Native ResourceQuotas are implementation details. Counting their
		// creation could prevent the quota authorizing them from initializing.
		return nil
	}

	applied := make([]appliedReservation, 0, len(quotaList))

	for _, quota := range quotaList {
		reservation, response := reserveForGlobalResourceQuota(ctx, c, reader, req, quota, evaluation)
		if response != nil {
			rollbackReservations(ctx, c, reader, applied)

			return response
		}

		if reservation != nil {
			applied = append(applied, *reservation)
		}
	}

	return nil
}

func reserveForGlobalResourceQuota(
	ctx context.Context,
	c client.Client,
	reader client.Reader,
	req admission.Request,
	quota *capsulev1beta2.GlobalResourceQuota,
	evaluation quotaevaluator.Result,
) (*appliedReservation, *admission.Response) {
	if quota.DeletionTimestamp != nil {
		return nil, nil
	}

	oldUsage, newUsage, err := usageForQuota(quota.Spec.Quota, evaluation)
	if err != nil {
		return nil, ad.Denyf(
			"resource cannot be evaluated against GlobalResourceQuota %q: %v",
			quota.Name,
			err,
		)
	}

	if !resourceListPositive(newUsage) && !resourceListPositive(oldUsage) {
		return nil, nil
	}

	delta := positiveDifference(newUsage, oldUsage)
	if !resourceListPositive(delta) {
		return nil, nil
	}

	ledgerKey := types.NamespacedName{
		Namespace: configuration.ControllerNamespace(),
		Name:      quota.GetLedgerName(),
	}
	reservation := newReservation(req, quota.Name, newUsage, delta)

	allowed, projected, applied, err := reserve(
		ctx,
		c,
		reader,
		ledgerKey,
		quota,
		reservation,
		req.DryRun != nil && *req.DryRun,
	)
	if err != nil {
		if apierrors.IsNotFound(err) {
			return nil, ad.Denyf(
				"GlobalResourceQuota %q is not ready: QuantityLedger %s does not exist",
				quota.Name,
				ledgerKey.String(),
			)
		}

		return nil, ad.ErroredResponse(err)
	}

	if !allowed {
		return nil, ad.Denyf(
			"resource exceeds GlobalResourceQuota %q: %s",
			quota.Name,
			formatExceededResources(delta, projected, quota.Spec.Quota.Hard),
		)
	}

	if !applied || (req.DryRun != nil && *req.DryRun) {
		return nil, nil
	}

	return &appliedReservation{Key: ledgerKey, ID: reservation.ID}, nil
}

func isGlobalResourceQuotaRequest(req admission.Request) bool {
	return req.Resource.Group == capsulev1beta2.GroupVersion.Group &&
		req.Resource.Resource == "globalresourcequotas" &&
		req.SubResource == ""
}

func validateGlobalResourceQuota(quota *capsulev1beta2.GlobalResourceQuota) error {
	if len(quota.Spec.Quota.Hard) == 0 {
		return fmt.Errorf("spec.quota.hard must contain at least one resource")
	}

	for name, quantity := range quota.Spec.Quota.Hard {
		if quantity.Sign() < 0 {
			return fmt.Errorf("spec.quota.hard[%q] must not be negative", name)
		}
	}

	for index, namespaceSelector := range quota.Spec.NamespaceSelectors {
		if namespaceSelector.LabelSelector == nil {
			continue
		}

		if _, err := metav1.LabelSelectorAsSelector(namespaceSelector.LabelSelector); err != nil {
			return fmt.Errorf("spec.namespaceSelectors[%d] is invalid: %w", index, err)
		}
	}

	return nil
}

func validateHardLimit(hard, allocated corev1.ResourceList) error {
	return runtimequota.ValidateHardLimit("spec.quota.hard", hard, allocated)
}

func isManagedResourceQuota(req admission.Request, object any) bool {
	if req.Resource.Group != "" || req.Resource.Resource != "resourcequotas" {
		return false
	}

	metadata, ok := object.(metav1.Object)
	if !ok {
		return false
	}

	objectLabels := metadata.GetLabels()

	return objectLabels[meta.NewManagedByCapsuleLabel] == meta.ValueController &&
		objectLabels[meta.GlobalResourceQuotaLabel] != ""
}

func matchingGlobalResourceQuotas(
	namespace *corev1.Namespace,
	quotas []capsulev1beta2.GlobalResourceQuota,
) ([]*capsulev1beta2.GlobalResourceQuota, error) {
	out := make([]*capsulev1beta2.GlobalResourceQuota, 0)
	namespaceLabels := labels.Set(namespace.Labels)

	for i := range quotas {
		quota := &quotas[i]

		for _, namespaceSelector := range quota.Spec.NamespaceSelectors {
			if namespaceSelector.LabelSelector == nil {
				continue
			}

			selector, err := metav1.LabelSelectorAsSelector(namespaceSelector.LabelSelector)
			if err != nil {
				return nil, fmt.Errorf("GlobalResourceQuota %q has an invalid namespace selector: %w", quota.Name, err)
			}

			if selector.Matches(namespaceLabels) {
				out = append(out, quota)

				break
			}
		}
	}

	return out, nil
}

func usageForQuota(
	spec corev1.ResourceQuotaSpec,
	evaluation quotaevaluator.Result,
) (corev1.ResourceList, corev1.ResourceList, error) {
	newUsage := corev1.ResourceList{}
	oldUsage := corev1.ResourceList{}

	if evaluation.New != nil {
		matches, err := quotaevaluator.MatchesScopes(spec, evaluation.New)
		if err != nil {
			return nil, nil, err
		}

		if matches {
			if err := quotaevaluator.ValidateConstraints(spec.Hard, evaluation.New); err != nil {
				return nil, nil, err
			}

			newUsage = maskResourceList(evaluation.NewUsage, spec.Hard)
		}
	}

	if evaluation.Old != nil {
		matches, err := quotaevaluator.MatchesScopes(spec, evaluation.Old)
		if err != nil {
			return nil, nil, err
		}

		if matches {
			oldUsage = maskResourceList(evaluation.OldUsage, spec.Hard)
		}
	}

	return oldUsage, newUsage, nil
}

func reserve(
	ctx context.Context,
	c client.Client,
	reader client.Reader,
	key types.NamespacedName,
	quota *capsulev1beta2.GlobalResourceQuota,
	reservation capsulev1beta2.QuantityLedgerResourceQuotaReservation,
	dryRun bool,
) (allowed bool, allocated corev1.ResourceList, applied bool, err error) {
	hard := quota.Spec.Quota.Hard

	err = retry.RetryOnConflict(ledgerBackoff, func() error {
		applied = false

		ledger := &capsulev1beta2.QuantityLedger{}
		if getErr := reader.Get(ctx, key, ledger); getErr != nil {
			return getErr
		}

		target := ledger.Spec.TargetRef
		if target.Kind != "GlobalResourceQuota" ||
			target.Name != quota.Name ||
			target.UID != quota.UID {
			return fmt.Errorf("QuantityLedger %s has a stale GlobalResourceQuota target", key.String())
		}

		if ledger.Status.ResourceQuota == nil || !ledger.Status.ResourceQuota.Initialized {
			return fmt.Errorf("GlobalResourceQuota QuantityLedger %s is not initialized", key.String())
		}

		if ledger.Status.ResourceQuota.ObservedGeneration != quota.Generation {
			return fmt.Errorf(
				"GlobalResourceQuota QuantityLedger %s has not observed generation %d",
				key.String(),
				quota.Generation,
			)
		}

		if !slices.Contains(ledger.Status.ResourceQuota.Namespaces, reservation.ObjectRef.Namespace) {
			return fmt.Errorf(
				"GlobalResourceQuota QuantityLedger %s has not observed namespace %s",
				key.String(),
				reservation.ObjectRef.Namespace,
			)
		}

		now := metav1.Now()
		active := make([]capsulev1beta2.QuantityLedgerResourceQuotaReservation, 0, len(ledger.Status.ResourceQuota.Reservations)+1)
		found := false

		for _, existing := range ledger.Status.ResourceQuota.Reservations {
			if existing.ExpiresAt != nil && existing.ExpiresAt.Before(&now) {
				continue
			}

			if existing.ID == reservation.ID {
				found = true
				applied = true
				existing.Usage = reservation.Usage.DeepCopy()
				existing.Delta = reservation.Delta.DeepCopy()
				existing.ObjectRef = reservation.ObjectRef
				existing.UpdatedAt = now
				existing.ExpiresAt = reservation.ExpiresAt
			}

			active = append(active, existing)
		}

		if !found {
			if len(active) >= maxReservations {
				return fmt.Errorf("GlobalResourceQuota QuantityLedger %s has too many inflight reservations", key.String())
			}

			active = append(active, reservation)
			applied = true
		}

		reserved := sumReservations(active, hard)
		next := ledger.Status.ResourceQuota.Used.DeepCopy()
		addResourceList(next, reserved)
		allocated = next.DeepCopy()

		if exceeds(next, hard) {
			allowed = false
			applied = false

			return nil
		}

		if dryRun {
			allowed = true
			applied = false

			return nil
		}

		ledger.Status.ResourceQuota.Reservations = active
		ledger.Status.ResourceQuota.Reserved = reserved
		ledger.Status.ResourceQuota.Allocated = next

		if updateErr := c.Status().Update(ctx, ledger); updateErr != nil {
			applied = false

			return updateErr
		}

		allowed = true

		return nil
	})

	return allowed, allocated, applied, err
}

func rollbackReservations(
	ctx context.Context,
	c client.Client,
	reader client.Reader,
	applied []appliedReservation,
) {
	for _, item := range slices.Backward(applied) {
		_ = rollbackReservation(ctx, c, reader, item.Key, item.ID)
	}
}

func rollbackReservation(
	ctx context.Context,
	c client.Client,
	reader client.Reader,
	key types.NamespacedName,
	id string,
) error {
	return retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		ledger := &capsulev1beta2.QuantityLedger{}
		if err := reader.Get(ctx, key, ledger); err != nil {
			if apierrors.IsNotFound(err) {
				return nil
			}

			return err
		}

		if ledger.Status.ResourceQuota == nil {
			return nil
		}

		active := make([]capsulev1beta2.QuantityLedgerResourceQuotaReservation, 0, len(ledger.Status.ResourceQuota.Reservations))
		removed := false

		for _, reservation := range ledger.Status.ResourceQuota.Reservations {
			if reservation.ID == id {
				removed = true

				continue
			}

			active = append(active, reservation)
		}

		if !removed {
			return nil
		}

		reserved := sumReservations(active, ledger.Status.ResourceQuota.Used)
		allocated := ledger.Status.ResourceQuota.Used.DeepCopy()
		addResourceList(allocated, reserved)

		ledger.Status.ResourceQuota.Reservations = active
		ledger.Status.ResourceQuota.Reserved = reserved
		ledger.Status.ResourceQuota.Allocated = allocated

		return c.Status().Update(ctx, ledger)
	})
}

func newReservation(
	req admission.Request,
	quotaKey string,
	usage corev1.ResourceList,
	delta corev1.ResourceList,
) capsulev1beta2.QuantityLedgerResourceQuotaReservation {
	now := metav1.Now()
	expires := metav1.NewTime(now.Add(2 * time.Minute))

	return capsulev1beta2.QuantityLedgerResourceQuotaReservation{
		ID: fmt.Sprintf("%s/%s", req.UID, quotaKey),
		ObjectRef: capsulev1beta2.QuantityLedgerObjectRef{
			APIGroup:   req.Kind.Group,
			APIVersion: req.Kind.Version,
			Kind:       req.Kind.Kind,
			Namespace:  req.Namespace,
			Name:       req.Name,
		},
		Usage:     usage.DeepCopy(),
		Delta:     delta.DeepCopy(),
		CreatedAt: now,
		UpdatedAt: now,
		ExpiresAt: &expires,
	}
}

func maskResourceList(usage, hard corev1.ResourceList) corev1.ResourceList {
	out := make(corev1.ResourceList)

	for name := range hard {
		if quantity, ok := usage[name]; ok {
			out[name] = quantity.DeepCopy()
		}
	}

	return out
}

func positiveDifference(next, previous corev1.ResourceList) corev1.ResourceList {
	out := make(corev1.ResourceList)

	for name, quantity := range next {
		delta := quantity.DeepCopy()
		delta.Sub(previous[name])
		runtimequota.ClampQuantityToZero(&delta)
		out[name] = delta
	}

	return out
}

func sumReservations(
	reservations []capsulev1beta2.QuantityLedgerResourceQuotaReservation,
	resources corev1.ResourceList,
) corev1.ResourceList {
	out := zeroResourceList(resources)
	for _, reservation := range reservations {
		addResourceList(out, reservation.Delta)
	}

	return out
}

func zeroResourceList(resources corev1.ResourceList) corev1.ResourceList {
	out := make(corev1.ResourceList, len(resources))
	for name := range resources {
		out[name] = *resource.NewQuantity(0, resource.DecimalSI)
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

func exceeds(usage, hard corev1.ResourceList) bool {
	for name, limit := range hard {
		quantity := usage[name]
		if quantity.Cmp(limit) > 0 {
			return true
		}
	}

	return false
}

func resourceListPositive(resources corev1.ResourceList) bool {
	for _, quantity := range resources {
		if quantity.Sign() > 0 {
			return true
		}
	}

	return false
}

func formatExceededResources(requested, projected, hard corev1.ResourceList) string {
	names := make([]corev1.ResourceName, 0, len(hard))

	for name, limit := range hard {
		projectedQuantity := quantityForResource(projected, name)
		if projectedQuantity.Cmp(limit) > 0 {
			names = append(names, name)
		}
	}

	slices.Sort(names)

	details := make([]string, 0, len(names))

	for _, name := range names {
		requestedQuantity := quantityForResource(requested, name)
		projectedQuantity := quantityForResource(projected, name)
		hardQuantity := quantityForResource(hard, name)

		currentQuantity := projectedQuantity.DeepCopy()
		currentQuantity.Sub(requestedQuantity)
		runtimequota.ClampQuantityToZero(&currentQuantity)

		exceededBy := projectedQuantity.DeepCopy()
		exceededBy.Sub(hardQuantity)

		details = append(details, fmt.Sprintf(
			"%s (requested=%s, current=%s, projected=%s, hard=%s, exceededBy=%s)",
			name,
			requestedQuantity.String(),
			currentQuantity.String(),
			projectedQuantity.String(),
			hardQuantity.String(),
			exceededBy.String(),
		))
	}

	return strings.Join(details, "; ")
}

func quantityForResource(resources corev1.ResourceList, name corev1.ResourceName) resource.Quantity {
	if quantity, found := resources[name]; found {
		return quantity.DeepCopy()
	}

	return *resource.NewQuantity(0, resource.DecimalSI)
}
