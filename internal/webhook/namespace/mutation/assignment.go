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
		var tnt *capsulev1beta2.Tenant

		if user.IsAdmin() {
			if !tenant.HasConsistentTenantReference(newNs) {
				return ad.Deny("tenant label and ownerReference must both be set consistently or both be absent")
			}

			if !tenant.HasTenantReference(newNs) {
				return nil
			}

			var err error
			tnt, err = tenant.ResolveNamespaceTenant(ctx, reader, newNs)
			if err != nil {
				return ad.ErroredResponse(err)
			}
		} else {
			var err error
			tnt, err = resolveTenantForNamespaceUpdate(ctx, reader, user, h.cfg, oldNs, newNs)
			if err != nil {
				return ad.ErroredResponse(err)
			}
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
		tenant.AddNamespaceNameLabels(labels, newNs)
		tenant.AddTenantNameLabel(labels, tnt)
		newNs.SetLabels(labels)

		return nil
	}
}

func resolveTenantForNamespaceUpdate(
	ctx context.Context,
	c client.Reader,
	user users.AdmissionUser,
	cfg configuration.Configuration,
	oldNs, newNs *corev1.Namespace,
) (*capsulev1beta2.Tenant, error) {
	// 1) try old ownerRefs
	if tnt, err := tenant.GetTenantByOwnerreferences(ctx, c, oldNs.OwnerReferences); err != nil {
		return nil, err
	} else if tnt != nil {
		return tnt, nil
	}

	// 2) try new ownerRefs
	if tnt, err := tenant.GetTenantByOwnerreferences(ctx, c, newNs.OwnerReferences); err != nil {
		return nil, err
	} else if tnt != nil {
		return tnt, nil
	}

	// 3) fall back to labels + user
	return tenant.GetTenantByLabelsAndUser(ctx, c, cfg, newNs, user)
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
