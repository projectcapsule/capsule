// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package validation

import (
	"context"
	"encoding/json"
	"slices"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	caperrors "github.com/projectcapsule/capsule/pkg/api/errors"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	ad "github.com/projectcapsule/capsule/pkg/runtime/admission"
	"github.com/projectcapsule/capsule/pkg/runtime/events"
	"github.com/projectcapsule/capsule/pkg/runtime/handlers"
	"github.com/projectcapsule/capsule/pkg/users"
)

// namespaceReservationTTL bounds how long an admitted Namespace counts against
// the quota before it shows up in the Tenant status. A reservation for a
// Namespace that is never persisted (denied by a later webhook) frees its slot
// once expired.
const namespaceReservationTTL = 2 * time.Minute

type quotaHandler struct{}

func QuotaHandler() handlers.TypedHandlerWithTenantUser[*corev1.Namespace] {
	return &quotaHandler{}
}

func (h *quotaHandler) OnCreate(
	c client.Client,
	reader client.Reader,
	_ users.AdmissionUser,
	ns *corev1.Namespace,
	_ admission.Decoder,
	recorder events.EventRecorder,
	tnt *capsulev1beta2.Tenant,
) handlers.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		return h.reserve(ctx, req, c, reader, recorder, ns, tnt)
	}
}

func (h *quotaHandler) OnDelete(
	client.Client,
	client.Reader,
	users.AdmissionUser,
	*corev1.Namespace,
	admission.Decoder,
	events.EventRecorder,
	*capsulev1beta2.Tenant,
) handlers.Func {
	return func(context.Context, admission.Request) *admission.Response {
		return nil
	}
}

func (h *quotaHandler) OnUpdate(
	_ client.Client,
	reader client.Reader,
	_ users.AdmissionUser,
	ns *corev1.Namespace,
	_ *corev1.Namespace,
	_ admission.Decoder,
	recorder events.EventRecorder,
	tnt *capsulev1beta2.Tenant,
) handlers.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		return h.handle(ctx, req, reader, recorder, ns, tnt)
	}
}

// reserve admits a Namespace creation only if the Tenant has a free slot, and
// takes that slot by recording the Namespace in the Tenant's reservations.
// The Tenant status only lists a Namespace once it exists, so concurrent
// creations would all see the same count: writing the reservation with the
// Tenant's resourceVersion makes them conflict, and each retry counts the
// reservations of the requests admitted before it.
func (h *quotaHandler) reserve(
	ctx context.Context,
	req admission.Request,
	c client.Client,
	reader client.Reader,
	recorder events.EventRecorder,
	ns *corev1.Namespace,
	tnt *capsulev1beta2.Tenant,
) *admission.Response {
	if tnt.Spec.NamespaceOptions == nil || tnt.Spec.NamespaceOptions.Quota == nil {
		return nil
	}

	if slices.Contains(tnt.Status.Namespaces, ns.Name) {
		// Already part of the Tenant: the API server answers AlreadyExists.
		return nil
	}

	full := false
	current := tnt.DeepCopy()

	err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		if current == nil {
			current = &capsulev1beta2.Tenant{}
			// Passing an option bypasses the per-request Tenant cache, so a
			// retry reads the version that conflicted.
			if err := reader.Get(ctx, types.NamespacedName{Name: tnt.Name}, current, &client.GetOptions{}); err != nil {
				return err
			}
		}

		tenant := current
		current = nil // a conflict re-reads the Tenant

		now := time.Now()
		reservations := activeNamespaceReservations(tenant, now)

		if _, reserved := reservations[ns.Name]; reserved {
			return nil // the same Namespace admitted again, e.g. a retried request
		}

		if len(tenant.Status.Namespaces)+len(reservations) >= int(*tenant.Spec.NamespaceOptions.Quota) {
			full = true

			return nil
		}

		if req.DryRun != nil && *req.DryRun {
			return nil
		}

		reservations[ns.Name] = now.Add(namespaceReservationTTL).UTC()

		value, err := json.Marshal(reservations)
		if err != nil {
			return err
		}

		if tenant.Annotations == nil {
			tenant.Annotations = map[string]string{}
		}

		tenant.Annotations[meta.NamespaceReservationsAnnotation] = string(value)

		return c.Update(ctx, tenant)
	})
	if err != nil {
		return ad.ErroredResponse(err)
	}

	if full {
		return h.deny(ctx, req, reader, recorder, ns, tnt)
	}

	return nil
}

// activeNamespaceReservations returns the reservations that still hold a slot:
// not expired, and for a Namespace not yet listed in the Tenant status.
func activeNamespaceReservations(tnt *capsulev1beta2.Tenant, now time.Time) map[string]time.Time {
	reservations := map[string]time.Time{}

	if value := tnt.Annotations[meta.NamespaceReservationsAnnotation]; value != "" {
		// A malformed value is rewritten with the next reservation.
		_ = json.Unmarshal([]byte(value), &reservations)
	}

	for name, expires := range reservations {
		if !expires.After(now) || slices.Contains(tnt.Status.Namespaces, name) {
			delete(reservations, name)
		}
	}

	return reservations
}

func (h *quotaHandler) handle(
	ctx context.Context,
	req admission.Request,
	c client.Reader,
	recorder events.EventRecorder,
	ns *corev1.Namespace,
	tnt *capsulev1beta2.Tenant,
) *admission.Response {
	if tnt.IsFull() {
		return h.deny(ctx, req, c, recorder, ns, tnt)
	}

	return nil
}

func (h *quotaHandler) deny(
	ctx context.Context,
	req admission.Request,
	c client.Reader,
	recorder events.EventRecorder,
	ns *corev1.Namespace,
	tnt *capsulev1beta2.Tenant,
) *admission.Response {
	// Checking if the Namespace already exists.
	// If this is the case, no need to return the quota exceeded error:
	// the Kubernetes API Server will return an AlreadyExists error,
	// adhering more to the native Kubernetes experience.
	if err := c.Get(ctx, types.NamespacedName{Name: ns.Name}, &corev1.Namespace{}); err == nil {
		return nil
	}

	recorder.LabeledEvent(
		ns,
		corev1.EventTypeWarning,
		events.ReasonOverprovision,
		events.ActionValidationDenied,
		"namespace cannot be attached, quota exceeded for the elected tenant",
	).
		WithRelated(tnt).
		WithTenantLabel(tnt).
		WithRequestAnnotations(req).
		Emit(ctx)

	return ad.Deny(caperrors.NewNamespaceQuotaExceededError().Error())
}
