// Copyright 2020-2025 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package mutation

import (
	"context"
	"encoding/json"
	"net/http"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/configuration"
	capsuleutils "github.com/projectcapsule/capsule/pkg/utils"
	capsulewebhook "github.com/projectcapsule/capsule/pkg/webhook"
	"github.com/projectcapsule/capsule/pkg/webhook/utils"
)

type ownerReferenceHandler struct {
	cfg configuration.Configuration
}

func OwnerReferenceHandler(cfg configuration.Configuration) capsulewebhook.Handler {
	return &ownerReferenceHandler{
		cfg: cfg,
	}
}

func (h *ownerReferenceHandler) OnCreate(client client.Client, decoder admission.Decoder, recorder record.EventRecorder) capsulewebhook.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		return h.setOwnerRef(ctx, req, client, decoder, recorder)
	}
}

func (h *ownerReferenceHandler) OnDelete(client.Client, admission.Decoder, record.EventRecorder) capsulewebhook.Func {
	return func(context.Context, admission.Request) *admission.Response {
		return nil
	}
}

func (h *ownerReferenceHandler) OnUpdate(c client.Client, decoder admission.Decoder, recorder record.EventRecorder) capsulewebhook.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		oldNs := &corev1.Namespace{}
		if err := decoder.DecodeRaw(req.OldObject, oldNs); err != nil {
			return utils.ErroredResponse(err)
		}

		tntList := &capsulev1beta2.TenantList{}
		if err := c.List(ctx, tntList, client.MatchingFieldsSelector{
			Selector: fields.OneTermEqualSelector(".status.namespaces", oldNs.Name),
		}); err != nil {
			return utils.ErroredResponse(err)
		}

		ok, err := h.namespaceIsOwned(ctx, c, oldNs, tntList, req)
		if err != nil {
			return utils.ErroredResponse(err)
		}

		if !ok {
			recorder.Eventf(oldNs, corev1.EventTypeWarning, "OfflimitNamespace", "Namespace %s can not be patched", oldNs.GetName())

			response := admission.Denied("Denied patch request for this namespace")

			return &response
		}

		newNs := &corev1.Namespace{}
		if err := decoder.Decode(req, newNs); err != nil {
			return utils.ErroredResponse(err)
		}

		o, err := json.Marshal(newNs.DeepCopy())
		if err != nil {
			response := admission.Errored(http.StatusInternalServerError, err)

			return &response
		}

		var refs []metav1.OwnerReference

		for _, ref := range oldNs.OwnerReferences {
			if capsuleutils.IsTenantOwnerReference(ref) {
				refs = append(refs, ref)
			}
		}

		for _, ref := range newNs.OwnerReferences {
			if !capsuleutils.IsTenantOwnerReference(ref) {
				refs = append(refs, ref)
			}
		}

		newNs.OwnerReferences = refs

		c, err := json.Marshal(newNs)
		if err != nil {
			response := admission.Errored(http.StatusInternalServerError, err)

			return &response
		}

		response := admission.PatchResponseFromRaw(o, c)

		return &response
	}
}

func (h *ownerReferenceHandler) namespaceIsOwned(ctx context.Context, c client.Client, ns *corev1.Namespace, tenantList *capsulev1beta2.TenantList, req admission.Request) (bool, error) {
	for _, tenant := range tenantList.Items {
		for _, ownerRef := range ns.OwnerReferences {
			if !capsuleutils.IsTenantOwnerReference(ownerRef) {
				continue
			}

			ok, err := utils.IsTenantOwner(ctx, c, &tenant, req.UserInfo, h.cfg.AllowServiceAccountPromotion())
			if err != nil {
				return false, err
			}

			if ownerRef.UID == tenant.UID && ok {
				return true, nil
			}
		}
	}

	return false, nil
}

func (h *ownerReferenceHandler) setOwnerRef(ctx context.Context, req admission.Request, client client.Client, decoder admission.Decoder, recorder record.EventRecorder) *admission.Response {
	ns := &corev1.Namespace{}
	if err := decoder.Decode(req, ns); err != nil {
		response := admission.Errored(http.StatusBadRequest, err)

		return &response
	}

	ln, err := capsuleutils.GetTypeLabel(&capsulev1beta2.Tenant{})
	if err != nil {
		response := admission.Errored(http.StatusBadRequest, err)

		return &response
	}

	tnt, errResponse := getNamespaceTenant(ctx, client, ns, req, h.cfg, recorder)
	if errResponse != nil {
		return errResponse
	}

	if tnt == nil {
		response := admission.Denied("Unable to assign namespace to tenant. Please use " + ln + " label when creating a namespace")

		return &response
	}

	response := h.patchResponseForOwnerRef(tnt.DeepCopy(), ns, recorder)

	return &response
}

func (h *ownerReferenceHandler) patchResponseForOwnerRef(tenant *capsulev1beta2.Tenant, ns *corev1.Namespace, recorder record.EventRecorder) admission.Response {
	scheme := runtime.NewScheme()
	_ = capsulev1beta2.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	o, err := json.Marshal(ns.DeepCopy())
	if err != nil {
		return admission.Errored(http.StatusInternalServerError, err)
	}

	if err = controllerutil.SetOwnerReference(tenant, ns, scheme); err != nil {
		recorder.Eventf(tenant, corev1.EventTypeWarning, "Error", "Namespace %s cannot be assigned to the desired Tenant", ns.GetName())

		return admission.Errored(http.StatusInternalServerError, err)
	}

	recorder.Eventf(tenant, corev1.EventTypeNormal, "NamespaceCreationWebhook", "Namespace %s has been assigned to the desired Tenant", ns.GetName())

	c, err := json.Marshal(ns)
	if err != nil {
		return admission.Errored(http.StatusInternalServerError, err)
	}

	return admission.PatchResponseFromRaw(o, c)
}
