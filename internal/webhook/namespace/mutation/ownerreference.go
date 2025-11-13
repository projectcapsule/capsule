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
	"github.com/projectcapsule/capsule/pkg/configuration"
	capsuleutils "github.com/projectcapsule/capsule/pkg/utils"
	"github.com/projectcapsule/capsule/pkg/utils/tenant"
	"github.com/projectcapsule/capsule/pkg/utils/users"
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
		return HandleCreateOwnerReference(ctx, client, h.cfg, req, decoder, recorder)
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

		tnt, err := tenant.TenantByStatusNamespace(ctx, c, req.Name)
		if err != nil {
			return utils.ErroredResponse(err)
		}

		if tnt == nil {
			return nil
		}

		newNs := &corev1.Namespace{}
		if err := decoder.Decode(req, newNs); err != nil {
			return utils.ErroredResponse(err)
		}

		return HandleUpdateOwnerReference(
			ctx,
			c,
			h.cfg,
			tnt,
			newNs,
			oldNs,
			req,
			decoder,
			recorder,
		)
	}
}

func HandleUpdateOwnerReference(
	ctx context.Context,
	c client.Client,
	cfg configuration.Configuration,
	tnt *capsulev1beta2.Tenant,
	ns *corev1.Namespace,
	oldNs *corev1.Namespace,
	req admission.Request,
	decoder admission.Decoder,
	recorder record.EventRecorder,
) *admission.Response {
	ok, err := namespaceIsOwned(ctx, c, cfg, oldNs, tnt, req)
	if err != nil {
		return utils.ErroredResponse(err)
	}

	if !ok {
		recorder.Eventf(oldNs, corev1.EventTypeWarning, "OfflimitNamespace", "Namespace %s can not be patched", oldNs.GetName())

		response := admission.Denied("Denied patch request for this namespace")

		return &response
	}

	o, err := json.Marshal(ns.DeepCopy())
	if err != nil {
		response := admission.Errored(http.StatusInternalServerError, err)

		return &response
	}

	var refs []metav1.OwnerReference

	for _, ref := range oldNs.OwnerReferences {
		if tenant.IsTenantOwnerReference(ref) {
			refs = append(refs, ref)
		}
	}

	for _, ref := range ns.OwnerReferences {
		if !tenant.IsTenantOwnerReference(ref) {
			refs = append(refs, ref)
		}
	}

	ns.OwnerReferences = refs

	obj, err := json.Marshal(ns)
	if err != nil {
		response := admission.Errored(http.StatusInternalServerError, err)

		return &response
	}

	response := admission.PatchResponseFromRaw(o, obj)

	return &response

}

func namespaceIsOwned(
	ctx context.Context,
	c client.Client,
	cfg configuration.Configuration,
	ns *corev1.Namespace,
	tnt *capsulev1beta2.Tenant,
	req admission.Request,
) (bool, error) {
	for _, ownerRef := range ns.OwnerReferences {
		if !tenant.IsTenantOwnerReference(ownerRef) {
			continue
		}

		ok, err := users.IsTenantOwner(ctx, c, tnt, req.UserInfo, cfg.AllowServiceAccountPromotion())
		if err != nil {
			return false, err
		}

		if ownerRef.UID == tnt.UID && ok {
			return true, nil
		}
	}

	return false, nil
}

func HandleCreateOwnerReference(
	ctx context.Context,
	c client.Client,
	cfg configuration.Configuration,
	req admission.Request,
	decoder admission.Decoder,
	recorder record.EventRecorder,
) *admission.Response {
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

	tnt, errResponse := utils.GetNamespaceTenant(ctx, c, ns, req, cfg, recorder)
	if errResponse != nil {
		return errResponse
	}

	if tnt == nil {
		response := admission.Denied("Unable to assign namespace to tenant. Please use " + ln + " label when creating a namespace")

		return &response
	}

	response := patchResponseForOwnerRef(c, tnt.DeepCopy(), ns, recorder)

	return &response
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
