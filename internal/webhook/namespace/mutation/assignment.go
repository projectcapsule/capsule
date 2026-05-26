// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package mutation

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/events"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	ad "github.com/projectcapsule/capsule/pkg/runtime/admission"
	"github.com/projectcapsule/capsule/pkg/runtime/configuration"
	evt "github.com/projectcapsule/capsule/pkg/runtime/events"
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
		tnt, err := resolveTenantForNamespaceCreate(ctx, reader, user, h.cfg, ns)
		if err != nil {
			return ad.ErroredResponse(err)
		}

		if tnt == nil {
			response := admission.Denied("Unable to assign namespace to tenant. Please use " + meta.TenantLabel + " label when creating a namespace")

			return &response
		}

		labels := ns.GetLabels()
		tenant.AddNamespaceNameLabels(labels, ns)
		tenant.AddTenantNameLabel(labels, tnt)
		ns.SetLabels(labels)

		if err := assignToTenant(c, tnt, ns, recorder); err != nil {
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
		tnt, err := resolveTenantForNamespaceUpdate(ctx, reader, user, h.cfg, oldNs, newNs)
		if err != nil {
			return ad.ErroredResponse(err)
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

		if err := assignToTenant(c, tnt, newNs, recorder); err != nil {
			return ad.ErroredResponse(err)
		}

		labels := newNs.GetLabels()
		tenant.AddNamespaceNameLabels(labels, oldNs)
		tenant.AddTenantNameLabel(labels, tnt)
		newNs.SetLabels(labels)

		return nil
	}
}

func resolveTenantForNamespaceCreate(
	ctx context.Context,
	c client.Reader,
	user users.AdmissionUser,
	cfg configuration.Configuration,
	ns *corev1.Namespace,
) (*capsulev1beta2.Tenant, error) {
	if user.IsAdmin() {
		return tenant.GetTenantByLabels(ctx, c, ns)
	}

	return tenant.GetTenantByLabelsAndUser(ctx, c, cfg, ns, user)
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

	// 3) Controller/admin is allowed to resolve by label only.
	if user.IsAdmin() {
		return tenant.GetTenantByLabels(ctx, c, newNs)
	}

	// 4) fall back to labels + user
	return tenant.GetTenantByLabelsAndUser(ctx, c, cfg, newNs, user)
}

func assignToTenant(
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
		recorder.Eventf(ns, nil, corev1.EventTypeWarning, evt.ReasonNamespaceHijack, evt.ActionValidationDenied, "Namespace %s cannot be assigned to the desired tenant %s", ns.GetName(), tnt.GetName())

		return err
	}

	recorder.Eventf(ns, nil, corev1.EventTypeNormal, evt.ReasonTenantAssigned, evt.ActionMutated, "Namespace %s has been assigned to the desired tenant %s", ns.GetName(), tnt.GetName())

	return nil
}
