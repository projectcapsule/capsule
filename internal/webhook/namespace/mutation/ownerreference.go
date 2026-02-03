// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package mutation

import (
	"context"
	"encoding/json"
	"net/http"

	authenticationv1 "k8s.io/api/authentication/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/events"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/internal/webhook/utils"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/runtime/configuration"
	evt "github.com/projectcapsule/capsule/pkg/runtime/events"
	"github.com/projectcapsule/capsule/pkg/runtime/handlers"
	"github.com/projectcapsule/capsule/pkg/tenant"
)

type ownerReferenceHandler struct {
	cfg configuration.Configuration
}

func OwnerReferenceHandler(cfg configuration.Configuration) handlers.TypedHandler[*corev1.Namespace] {
	return &ownerReferenceHandler{
		cfg: cfg,
	}
}

func (h *ownerReferenceHandler) OnCreate(c client.Client, ns *corev1.Namespace, decoder admission.Decoder, recorder events.EventRecorder) handlers.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		tnt, errResponse := utils.GetNamespaceTenant(ctx, c, ns, req, h.cfg, recorder)
		if errResponse != nil {
			return errResponse
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
			return utils.ErroredResponse(err)
		}

		marshaled, err := json.Marshal(ns)
		if err != nil {
			response := admission.Errored(http.StatusInternalServerError, err)

			return &response
		}

		response := admission.PatchResponseFromRaw(req.Object.Raw, marshaled)

		return &response
	}
}

func (h *ownerReferenceHandler) OnDelete(client.Client, *corev1.Namespace, admission.Decoder, events.EventRecorder) handlers.Func {
	return func(context.Context, admission.Request) *admission.Response {
		return nil
	}
}

func (h *ownerReferenceHandler) OnUpdate(c client.Client, newNs *corev1.Namespace, oldNs *corev1.Namespace, decoder admission.Decoder, recorder events.EventRecorder) handlers.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		tnt, err := resolveTenantForNamespaceUpdate(ctx, c, h.cfg, oldNs, newNs, req.UserInfo)
		if err != nil {
			return utils.ErroredResponse(err)
		}

		if tnt == nil {
			return nil
		}

		if err := assignToTenant(c, tnt, oldNs, recorder); err != nil {
			return utils.ErroredResponse(err)
		}

		var refs []metav1.OwnerReference

		for _, ref := range oldNs.OwnerReferences {
			if tenant.IsTenantOwnerReferenceForTenant(ref, tnt) {
				refs = append(refs, ref)
			}
		}

		for _, ref := range newNs.OwnerReferences {
			if !tenant.IsTenantOwnerReference(ref) {
				refs = append(refs, ref)
			}
		}

		newNs.OwnerReferences = refs

		labels := newNs.GetLabels()
		tenant.AddNamespaceNameLabels(labels, oldNs)
		tenant.AddTenantNameLabel(labels, tnt)
		newNs.SetLabels(labels)

		marshaled, err := json.Marshal(newNs)
		if err != nil {
			response := admission.Errored(http.StatusInternalServerError, err)

			return &response
		}

		response := admission.PatchResponseFromRaw(req.Object.Raw, marshaled)

		return &response
	}
}

func resolveTenantForNamespaceUpdate(
	ctx context.Context,
	c client.Client,
	cfg configuration.Configuration,
	oldNs, newNs *corev1.Namespace,
	userInfo authenticationv1.UserInfo,
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
	return tenant.GetTenantByLabelsAndUser(ctx, c, cfg, newNs, userInfo)
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
		recorder.Eventf(ns, tnt, corev1.EventTypeWarning, evt.ReasonNamespaceHijack, evt.ActionValidationDenied, "Namespace %s cannot be assigned to the desired tenant %s", ns.GetName(), tnt.GetName())

		return err
	}

	recorder.Eventf(ns, tnt, corev1.EventTypeNormal, evt.ReasonTenantAssigned, evt.ActionValidationDenied, "Namespace %s has been assigned to the desired tenant %s", ns.GetName(), tnt.GetName())

	return nil
}
