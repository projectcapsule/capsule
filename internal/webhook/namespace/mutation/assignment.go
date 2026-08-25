// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package mutation

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/internal/webhook/utils"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	ad "github.com/projectcapsule/capsule/pkg/runtime/admission"
	"github.com/projectcapsule/capsule/pkg/runtime/configuration"
	"github.com/projectcapsule/capsule/pkg/runtime/events"
	"github.com/projectcapsule/capsule/pkg/runtime/handlers"
	"github.com/projectcapsule/capsule/pkg/tenant"
	"github.com/projectcapsule/capsule/pkg/users"
)

type ownerReferenceHandler struct {
	cfg configuration.Configuration
}

func OwnerReferenceHandler(cfg configuration.Configuration) handlers.TypedHandlerWithUser[*corev1.Namespace] {
	return &ownerReferenceHandler{
		cfg: cfg,
	}
}

func (h *ownerReferenceHandler) OnCreate(
	c client.Client,
	reader client.Reader,
	user users.AdmissionUser,
	ns *corev1.Namespace,
	decoder admission.Decoder,
	recorder events.EventRecorder,
) handlers.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		tnt, errResponse := utils.GetNamespaceTenant(ctx, reader, c, ns, user, h.cfg, recorder)
		if errResponse != nil {
			return errResponse
		}

		// Administrators are allowed to create namespaces that are not managed
		// by Capsule. In that case there is intentionally no tenant to assign.
		if tnt == nil && user.IsAdmin() {
			return ad.Allow("")
		}

		if tnt == nil {
			return ad.Deny(
				"Unable to assign namespace to tenant. Please use " +
					meta.TenantLabel +
					" label when creating a namespace",
			)
		}

		labels := ns.GetLabels()
		tenant.AddNamespaceNameLabels(labels, ns)
		tenant.AddTenantNameLabel(labels, tnt)
		ns.SetLabels(labels)

		if err := assignToTenant(ctx, req, c, tnt, ns, recorder); err != nil {
			return ad.ErroredResponse(err)
		}

		return nil
	}
}

func (h *ownerReferenceHandler) OnDelete(
	client.Client,
	client.Reader,
	users.AdmissionUser,
	*corev1.Namespace,
	admission.Decoder,
	events.EventRecorder,
) handlers.Func {
	return func(context.Context, admission.Request) *admission.Response {
		return nil
	}
}

func (h *ownerReferenceHandler) OnUpdate(
	c client.Client,
	reader client.Reader,
	user users.AdmissionUser,
	newNs *corev1.Namespace,
	oldNs *corev1.Namespace,
	decoder admission.Decoder,
	recorder events.EventRecorder,
) handlers.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		tnt, errResponse := h.tenantForUpdate(ctx, reader, user, oldNs, newNs, req, recorder)
		if errResponse != nil {
			return errResponse
		}

		if tnt == nil {
			return nil
		}

		refs := make([]metav1.OwnerReference, 0, len(newNs.OwnerReferences))

		for _, ref := range newNs.OwnerReferences {
			if tenant.IsTenantOwnerReference(ref) && !tenant.IsTenantOwnerReferenceForTenant(ref, tnt) {
				continue
			}

			refs = append(refs, ref)
		}

		newNs.OwnerReferences = refs

		if err := assignToTenant(ctx, req, c, tnt, newNs, recorder); err != nil {
			return ad.ErroredResponse(err)
		}

		labels := newNs.GetLabels()
		if labels == nil {
			labels = make(map[string]string)
		}

		tenant.AddNamespaceNameLabels(labels, newNs)
		tenant.AddTenantNameLabel(labels, tnt)
		newNs.SetLabels(labels)

		return nil
	}
}

func (h *ownerReferenceHandler) tenantForUpdate(
	ctx context.Context,
	reader client.Reader,
	user users.AdmissionUser,
	oldNs, newNs *corev1.Namespace,
	req admission.Request,
	recorder events.EventRecorder,
) (*capsulev1beta2.Tenant, *admission.Response) {
	if user.IsAdmin() {
		requestedTenant, err := requestedNamespaceTenant(ctx, reader, newNs)
		if err != nil {
			return nil, denyNamespacePatch(ctx, req, oldNs, recorder, err.Error())
		}

		return requestedTenant, nil
	}

	oldTenant, err := tenant.ResolveNamespaceTenant(ctx, reader, oldNs)
	if err != nil {
		return nil, ad.ErroredResponse(err)
	}

	if oldTenant == nil {
		requestedTenant, requestedErr := requestedNamespaceTenant(ctx, reader, newNs)
		if requestedErr != nil {
			return nil, denyNamespacePatch(ctx, req, oldNs, recorder, requestedErr.Error())
		}

		if requestedTenant != nil {
			return nil, denyNamespacePatch(ctx, req, oldNs, recorder, "namespace can not be patched into a tenant")
		}

		return nil, denyNamespacePatch(ctx, req, oldNs, recorder, "namespace is not owned by any tenant")
	}

	if namespaceReferencesTenant(newNs, oldTenant) {
		if !tenant.NamespaceIsOwned(ctx, reader, h.cfg, oldNs, oldTenant, user) {
			return nil, denyNamespacePatch(ctx, req, oldNs, recorder, "denied patch request for this namespace")
		}

		return oldTenant, nil
	}

	requestedTenant, requestedErr := requestedNamespaceTenant(ctx, reader, newNs)
	if requestedErr != nil {
		return nil, denyNamespacePatch(ctx, req, oldNs, recorder, requestedErr.Error())
	}

	if requestedTenant == nil {
		return nil, denyNamespacePatch(ctx, req, oldNs, recorder, "namespace can not remove tenant ownership")
	}

	if oldTenant.GetName() != requestedTenant.GetName() || oldTenant.GetUID() != requestedTenant.GetUID() {
		return nil, denyNamespacePatch(ctx, req, oldNs, recorder, "namespace can not be migrated between tenants")
	}

	if !tenant.NamespaceIsOwned(ctx, reader, h.cfg, oldNs, oldTenant, user) {
		return nil, denyNamespacePatch(ctx, req, oldNs, recorder, "denied patch request for this namespace")
	}

	// Tenant owners may patch managed namespaces, but only administrators can
	// change their Tenant assignment. Returning the old Tenant lets this handler
	// normalize partial or attempted assignment changes back to the current one.
	return oldTenant, nil
}

func namespaceReferencesTenant(ns *corev1.Namespace, tnt *capsulev1beta2.Tenant) bool {
	if tenant.TenanLabelValue(ns) == tnt.GetName() {
		return true
	}

	for _, ref := range tenant.TenantOwnerReferences(ns) {
		if tenant.IsTenantOwnerReferenceForTenant(ref, tnt) {
			return true
		}
	}

	return false
}

func requestedNamespaceTenant(
	ctx context.Context,
	reader client.Reader,
	ns *corev1.Namespace,
) (*capsulev1beta2.Tenant, error) {
	label := tenant.TenanLabelValue(ns)
	refs := tenant.TenantOwnerReferences(ns)

	if len(refs) > 1 {
		return nil, fmt.Errorf("namespace can not have multiple Tenant ownerReferences")
	}

	name := label
	if len(refs) == 1 {
		if name != "" && name != refs[0].Name {
			return nil, fmt.Errorf("namespace label %q does not match owner reference %q", name, refs[0].Name)
		}

		name = refs[0].Name
	}

	if name == "" {
		return nil, nil
	}

	tnt := &capsulev1beta2.Tenant{}
	if err := reader.Get(ctx, client.ObjectKey{Name: name}, tnt); err != nil {
		return nil, err
	}

	if len(refs) == 1 && refs[0].UID != "" && refs[0].UID != tnt.GetUID() {
		return nil, fmt.Errorf(
			"tenant ownerReference UID mismatch for %q: namespace references UID %q but tenant has UID %q",
			name,
			refs[0].UID,
			tnt.GetUID(),
		)
	}

	return tnt, nil
}

func denyNamespacePatch(
	ctx context.Context,
	req admission.Request,
	ns *corev1.Namespace,
	recorder events.EventRecorder,
	message string,
) *admission.Response {
	if ns != nil {
		recorder.LabeledEvent(
			ns,
			corev1.EventTypeWarning,
			events.ReasonNamespaceHijack,
			events.ActionValidationDenied,
			"namespace disallows patching relevant metadata",
		).
			WithRequestAnnotations(req).
			Emit(ctx)
	}

	return ad.Deny(message)
}

func assignToTenant(
	ctx context.Context,
	req admission.Request,
	c client.Client,
	tnt *capsulev1beta2.Tenant,
	ns *corev1.Namespace,
	recorder events.EventRecorder,
) error {
	has, err := controllerutil.HasOwnerReference(ns.OwnerReferences, tnt, c.Scheme())
	if err != nil {
		return err
	}

	if has {
		return nil
	}

	if err := controllerutil.SetOwnerReference(tnt, ns, c.Scheme()); err != nil {
		recorder.LabeledEvent(
			ns,
			corev1.EventTypeWarning,
			events.ReasonAdmissionFailure,
			events.ActionValidationDenied,
			fmt.Sprintf("namespace cannot be assigned to the desired tenant %s", tnt.GetName()),
		).
			WithRequestAnnotations(req).
			Emit(ctx)

		return err
	}

	recorder.LabeledEvent(
		ns,
		corev1.EventTypeNormal,
		events.ReasonTenantAssigned,
		events.ActionMutated,
		fmt.Sprintf("namespace has been assigned to the desired tenant %s", tnt.GetName()),
	).
		WithRelated(tnt).
		WithTenantLabel(tnt).
		WithRequestAnnotations(req).
		Emit(ctx)

	return nil
}
