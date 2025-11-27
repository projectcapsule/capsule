// Copyright 2020-2025 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package mutation

import (
	"context"
	"encoding/json"
	"net/http"

	authenticationv1 "k8s.io/api/authentication/v1"
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

		labels := ns.GetLabels()
		tenant.AddNamespaceNameLabels(labels, ns)
		tenant.AddTenantNameLabel(labels, ns, tnt)
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

func (h *ownerReferenceHandler) OnDelete(client.Client, *corev1.Namespace, admission.Decoder, record.EventRecorder) capsulewebhook.Func {
	return func(context.Context, admission.Request) *admission.Response {
		return nil
	}
}

func (h *ownerReferenceHandler) OnUpdate(c client.Client, newNs *corev1.Namespace, oldNs *corev1.Namespace, decoder admission.Decoder, recorder record.EventRecorder) capsulewebhook.Func {
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
		tenant.AddTenantNameLabel(labels, oldNs, tnt)
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
	recorder record.EventRecorder,
) error {
	has, err := controllerutil.HasOwnerReference(ns.OwnerReferences, tnt, c.Scheme())
	if err != nil {
		return err
	}

	if has {
		return nil
	}

	if err := controllerutil.SetOwnerReference(tnt, ns, c.Scheme()); err != nil {
		recorder.Eventf(tnt, corev1.EventTypeWarning, "Error", "Namespace %s cannot be assigned to the desired Tenant", ns.GetName())

		return err
	}

	recorder.Eventf(tnt, corev1.EventTypeNormal, "NamespaceCreationWebhook", "Namespace %s has been assigned to the desired Tenant", ns.GetName())

	return nil
}
