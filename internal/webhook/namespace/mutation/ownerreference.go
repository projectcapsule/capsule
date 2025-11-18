// Copyright 2020-2025 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package mutation

import (
	"context"
	"encoding/json"
	"net/http"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	capsulewebhook "github.com/projectcapsule/capsule/internal/webhook"
	"github.com/projectcapsule/capsule/internal/webhook/utils"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/configuration"
	"github.com/projectcapsule/capsule/pkg/utils/tenant"
)

type ownerReferenceHandler struct {
	cfg configuration.Configuration
}

func OwnerReferenceHandler(cfg configuration.Configuration) capsulewebhook.TypedHandler[*corev1.Namespace] {
	return &ownerReferenceHandler{
		cfg: cfg,
	}
}

func (h *ownerReferenceHandler) OnCreate(c client.Client, ns *corev1.Namespace, decoder admission.Decoder, recorder record.EventRecorder) capsulewebhook.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		tnt, errResponse := utils.GetNamespaceTenant(ctx, c, ns, req, h.cfg, recorder)
		if errResponse != nil {
			return errResponse
		}

		if tnt == nil {
			response := admission.Denied("Unable to assign namespace to tenant. Please use " + meta.TenantLabel + " label when creating a namespace")

			return &response
		}

		if err := tenant.AddTenantLabelForNamespace(ns, tnt); err != nil {
			response := admission.Errored(http.StatusBadRequest, err)

			return &response
		}

		response := patchResponseForOwnerRef(c, tnt.DeepCopy(), ns, recorder)

		return &response
	}
}

func (h *ownerReferenceHandler) OnDelete(client.Client, *corev1.Namespace, admission.Decoder, record.EventRecorder) capsulewebhook.Func {
	return func(context.Context, admission.Request) *admission.Response {
		return nil
	}
}

func (h *ownerReferenceHandler) OnUpdate(c client.Client, newNs *corev1.Namespace, oldNs *corev1.Namespace, decoder admission.Decoder, recorder record.EventRecorder) capsulewebhook.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		tnt, err := tenant.GetTenantByOwnerreferences(ctx, c, oldNs.OwnerReferences)
		if err != nil {
			return utils.ErroredResponse(err)
		}

		if tnt == nil {
			return nil
		}

		o, err := json.Marshal(newNs.DeepCopy())
		if err != nil {
			response := admission.Errored(http.StatusInternalServerError, err)

			return &response
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

		if err := tenant.AddTenantLabelForNamespace(newNs, tnt); err != nil {
			response := admission.Errored(http.StatusBadRequest, err)

			return &response
		}

		obj, err := json.Marshal(newNs)
		if err != nil {
			response := admission.Errored(http.StatusInternalServerError, err)

			return &response
		}

		response := admission.PatchResponseFromRaw(o, obj)

		return &response
	}
}

func patchResponseForOwnerRef(
	c client.Client,
	tenant *capsulev1beta2.Tenant,
	ns *corev1.Namespace,
	recorder record.EventRecorder,
) admission.Response {
	o, err := json.Marshal(ns.DeepCopy())
	if err != nil {
		return admission.Errored(http.StatusInternalServerError, err)
	}

	if err = controllerutil.SetOwnerReference(tenant, ns, c.Scheme()); err != nil {
		recorder.Eventf(tenant, corev1.EventTypeWarning, "Error", "Namespace %s cannot be assigned to the desired Tenant", ns.GetName())

		return admission.Errored(http.StatusInternalServerError, err)
	}

	recorder.Eventf(tenant, corev1.EventTypeNormal, "NamespaceCreationWebhook", "Namespace %s has been assigned to the desired Tenant", ns.GetName())

	obj, err := json.Marshal(ns)
	if err != nil {
		return admission.Errored(http.StatusInternalServerError, err)
	}

	return admission.PatchResponseFromRaw(o, obj)
}
