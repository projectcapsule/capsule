// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package validation

import (
	"context"
	"maps"
	"reflect"
	"slices"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/util/sets"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	webhookutils "github.com/projectcapsule/capsule/internal/webhook/utils"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	ad "github.com/projectcapsule/capsule/pkg/runtime/admission"
	"github.com/projectcapsule/capsule/pkg/runtime/configuration"
	"github.com/projectcapsule/capsule/pkg/runtime/events"
	"github.com/projectcapsule/capsule/pkg/runtime/handlers"
	"github.com/projectcapsule/capsule/pkg/tenant"
	"github.com/projectcapsule/capsule/pkg/users"
)

func NamespaceHandler(configuration configuration.Configuration, hndlers ...handlers.TypedHandlerWithTenantUser[*corev1.Namespace]) handlers.Handler {
	return &handler{
		cfg:      configuration,
		handlers: hndlers,
	}
}

type handler struct {
	cfg      configuration.Configuration
	handlers []handlers.TypedHandlerWithTenantUser[*corev1.Namespace]
}

func (h *handler) OnCreate(
	c client.Client,
	reader client.Reader,
	decoder admission.Decoder,
	recorder events.EventRecorder,
) handlers.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		reader = webhookutils.NewTenantCachingReader(reader)

		user := handlers.ResolveAdmissionUser(ctx, c, req, h.cfg)

		ns := &corev1.Namespace{}
		if err := decoder.Decode(req, ns); err != nil {
			return ad.ErroredResponse(err)
		}

		if !user.IsAdmin() && !user.IsCapsule() && !tenant.HasTenantReference(ns) {
			return nil
		}

		tnt, err := tenant.ResolveNamespaceTenant(ctx, reader, ns)
		if err != nil {
			return ad.ErroredResponse(err)
		}

		if !user.IsAdmin() && !user.IsCapsule() && tnt != nil {
			return ad.Deny("only tenant owners can create tenant-owned namespaces")
		}

		if tnt == nil {
			return nil
		}

		if response := validateTenantAssignment(nil, tnt); response != nil {
			return response
		}

		for _, hndl := range h.handlers {
			if response := hndl.OnCreate(c, reader, user, ns, decoder, recorder, tnt)(ctx, req); response != nil {
				return response
			}
		}

		return nil
	}
}

func (h *handler) OnDelete(
	c client.Client,
	reader client.Reader,
	decoder admission.Decoder,
	recorder events.EventRecorder,
) handlers.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		oldNs := &corev1.Namespace{}
		if err := decoder.DecodeRaw(req.OldObject, oldNs); err != nil {
			return ad.ErroredResponse(err)
		}

		// A namespace which is already terminating must be allowed to complete
		// deletion even if its Tenant metadata became inconsistent meanwhile.
		// Kubernetes authorization has already guarded the original delete.
		if oldNs.DeletionTimestamp != nil || oldNs.Status.Phase == corev1.NamespaceTerminating {
			return nil
		}

		reader = webhookutils.NewTenantCachingReader(reader)

		tnt, err := tenant.ResolveNamespaceTenant(ctx, reader, oldNs)
		if apierrors.IsNotFound(err) {
			// Kubernetes authorization already controls namespace deletion.
			// A stale Tenant reference must not make a namespace undeletable.
			return nil
		}

		user := handlers.ResolveAdmissionUser(ctx, c, req, h.cfg)

		if err != nil && !user.IsAdmin() {
			return ad.ErroredResponse(err)
		}

		if tnt == nil {
			return nil
		}

		for _, hndl := range h.handlers {
			if response := hndl.OnDelete(c, reader, user, oldNs, decoder, recorder, tnt)(ctx, req); response != nil {
				return response
			}
		}

		return nil
	}
}

func (h *handler) OnUpdate(
	c client.Client,
	reader client.Reader,
	decoder admission.Decoder,
	recorder events.EventRecorder,
) handlers.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		ns := &corev1.Namespace{}
		if err := decoder.Decode(req, ns); err != nil {
			return ad.ErroredResponse(err)
		}

		oldNs := &corev1.Namespace{}
		if err := decoder.DecodeRaw(req.OldObject, oldNs); err != nil {
			return ad.ErroredResponse(err)
		}

		if response, stop := validateTerminatingNamespaceUpdate(req, oldNs, ns); stop {
			return response
		}

		user := handlers.ResolveAdmissionUser(ctx, c, req, h.cfg)

		// Kubernetes authorizes status/finalize before admission. Its namespace
		// controller must finish cleanup even when a CREATE persisted after the
		// Tenant's final absence check. Keep tenant-owner checks and all metadata
		// changes on the ordinary path, except removal of existing finalizers.
		if !user.IsCapsule() && namespaceLifecycleOnlyUpdate(req, oldNs, ns) {
			return nil
		}

		if reader != nil {
			reader = webhookutils.NewTenantCachingReader(reader)
		}

		if response, stop := validateNamespaceTenantReferenceTransition(user, oldNs, ns); stop {
			return response
		}

		oldTenant, err := tenant.ResolveNamespaceTenant(ctx, reader, oldNs)
		if err != nil && !user.IsAdmin() {
			return ad.ErroredResponse(err)
		}

		newTenant, err := tenant.ResolveNamespaceTenant(ctx, reader, ns)
		if err != nil {
			return ad.ErroredResponse(err)
		}

		//nolint:nestif
		if !user.IsAdmin() {
			if oldTenant == nil || newTenant == nil {
				if isTerminatingNamespaceUpdate(oldNs, ns) {
					return nil
				}

				return ad.Deny("namespace tenant ownership is incomplete")
			}

			if oldTenant.GetName() != newTenant.GetName() || oldTenant.GetUID() != newTenant.GetUID() {
				return ad.Deny("namespace can not be migrated between tenants")
			}

			if user.IsCapsule() && !tenant.NamespaceIsOwned(ctx, c, h.cfg, oldNs, oldTenant, user) {
				recorder.LabeledEvent(
					ns,
					corev1.EventTypeWarning,
					events.ReasonNamespaceHijack,
					events.ActionValidationDenied,
					"namespace can not be patched",
				).
					WithRequestAnnotations(req).
					Emit(ctx)

				return ad.Deny("denied patch request for this namespace")
			}
		}

		adminTenantTransition := user.IsAdmin() && namespaceTenantChanged(oldTenant, newTenant)

		tnt := newTenant
		if !user.IsAdmin() {
			tnt = oldTenant
		}

		if tnt == nil {
			return nil
		}

		if response := validateTenantAssignment(oldNs, newTenant); response != nil {
			return response
		}

		if adminTenantTransition {
			return nil
		}

		for _, hndl := range h.handlers {
			if response := hndl.OnUpdate(c, reader, user, ns, oldNs, decoder, recorder, tnt)(ctx, req); response != nil {
				return response
			}
		}

		return nil
	}
}

func namespaceTenantChanged(oldTenant, newTenant *capsulev1beta2.Tenant) bool {
	if oldTenant == nil || newTenant == nil {
		return oldTenant != newTenant
	}

	return oldTenant.GetName() != newTenant.GetName() || oldTenant.GetUID() != newTenant.GetUID()
}

func isTerminatingNamespaceUpdate(
	oldNs, newNs *corev1.Namespace,
) bool {
	return newNs.DeletionTimestamp != nil ||
		oldNs.DeletionTimestamp != nil ||
		newNs.Status.Phase == corev1.NamespaceTerminating ||
		oldNs.Status.Phase == corev1.NamespaceTerminating
}

func validateTerminatingNamespaceUpdate(
	req admission.Request,
	oldNs, newNs *corev1.Namespace,
) (*admission.Response, bool) {
	terminating := isTerminatingNamespaceUpdate(oldNs, newNs)

	if !terminating && req.SubResource != "finalize" {
		return nil, false
	}

	if namespaceTenantAssignmentChanged(oldNs, newNs) {
		return ad.Deny("namespace tenant ownership can not change during termination"), true
	}

	return nil, false
}

func validateNamespaceTenantReferenceTransition(
	user users.AdmissionUser,
	oldNs, newNs *corev1.Namespace,
) (*admission.Response, bool) {
	if user.IsAdmin() {
		if !tenant.HasConsistentTenantReference(newNs) {
			return ad.Deny("tenant label and ownerReference must both be set consistently or both be absent"), true
		}

		return nil, false
	}

	oldHasTenantReference := tenant.HasTenantReference(oldNs)
	newHasTenantReference := tenant.HasTenantReference(newNs)

	switch {
	case !oldHasTenantReference && newHasTenantReference:
		return ad.Deny("namespace can not be patched into a tenant"), true
	case oldHasTenantReference && !newHasTenantReference:
		return ad.Deny("namespace can not remove tenant ownership"), true
	case !oldHasTenantReference && !newHasTenantReference:
		if user.IsCapsule() {
			return ad.Deny("namespace is not owned by any tenant"), true
		}

		return nil, true
	default:
		return nil, false
	}
}

func namespaceLifecycleOnlyUpdate(req admission.Request, oldNs, newNs *corev1.Namespace) bool {
	if oldNs.DeletionTimestamp == nil || newNs.DeletionTimestamp == nil ||
		(req.SubResource != "" && req.SubResource != "status" && req.SubResource != "finalize") {
		return false
	}

	if req.SubResource == "" && len(newNs.Finalizers) >= len(oldNs.Finalizers) {
		return false
	}

	// Typed map equality avoids reflection allocations proportional to metadata.
	if !maps.Equal(oldNs.Labels, newNs.Labels) || !maps.Equal(oldNs.Annotations, newNs.Annotations) {
		return false
	}

	// Compare metadata without copying its maps/slices. Only API-managed fields
	// may differ, alongside removal of existing finalizers. Other metadata still
	// needs Tenant validation when it changes.
	if len(newNs.Finalizers) > len(oldNs.Finalizers) ||
		(!slices.Equal(oldNs.Finalizers, newNs.Finalizers) && !sets.New(oldNs.Finalizers...).HasAll(newNs.Finalizers...)) {
		return false
	}

	oldMeta, newMeta := oldNs.ObjectMeta, newNs.ObjectMeta
	oldMeta.ResourceVersion = newMeta.ResourceVersion
	oldMeta.Generation = newMeta.Generation
	oldMeta.ManagedFields = newMeta.ManagedFields
	oldMeta.Finalizers = newMeta.Finalizers
	oldMeta.Labels, newMeta.Labels = nil, nil
	oldMeta.Annotations, newMeta.Annotations = nil, nil

	if !reflect.DeepEqual(oldMeta, newMeta) {
		return false
	}

	if req.SubResource == "status" {
		return reflect.DeepEqual(oldNs.Spec, newNs.Spec)
	}

	if !reflect.DeepEqual(oldNs.Status, newNs.Status) {
		return false
	}

	if req.SubResource == "" {
		return reflect.DeepEqual(oldNs.Spec, newNs.Spec)
	}

	// Finalization may remove existing spec finalizers, never introduce new ones.
	return len(newNs.Spec.Finalizers) <= len(oldNs.Spec.Finalizers) && sets.New(oldNs.Spec.Finalizers...).HasAll(newNs.Spec.Finalizers...)
}

func namespaceTenantAssignmentChanged(oldNs, newNs *corev1.Namespace) bool {
	if tenant.TenanLabelValue(oldNs) != tenant.TenanLabelValue(newNs) {
		return true
	}

	return !reflect.DeepEqual(
		tenant.TenantOwnerReferences(oldNs),
		tenant.TenantOwnerReferences(newNs),
	)
}

func validateTenantAssignment(
	oldNs *corev1.Namespace,
	t *capsulev1beta2.Tenant,
) *admission.Response {
	if t == nil {
		return nil
	}

	// Existing namespaces must be able to finish deletion even if they never
	// reached status.spaces. Use the persisted old ownership, including UID,
	// rather than the incoming owner reference or the Tenant status projection.
	if oldNs != nil {
		for _, ref := range oldNs.OwnerReferences {
			if tenant.IsTenantOwnerReferenceForTenant(ref, t) {
				return nil
			}
		}
	}

	if t.DeletionTimestamp != nil {
		return ad.Deny("tenant is terminating and does not accept new namespaces")
	}

	if !controllerutil.ContainsFinalizer(t, meta.ControllerFinalizer) {
		return ad.Deny("tenant lifecycle protection is not ready; retry after the Tenant is reconciled")
	}

	return nil
}
