// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package namespace

import (
	"context"
	"net/http"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulev1alpha1 "github.com/clastix/capsule/api/v1alpha1"
	"github.com/clastix/capsule/pkg/configuration"
	capsulewebhook "github.com/clastix/capsule/pkg/webhook"
	"github.com/clastix/capsule/pkg/webhook/utils"
)

// +kubebuilder:webhook:path=/validate-v1-namespace-freezed,mutating=false,sideEffects=None,admissionReviewVersions=v1,failurePolicy=fail,groups="",resources=namespaces,verbs=create;update;delete,versions=v1,name=freezed.namespace.capsule.clastix.io

type freezedWebhook struct {
	handler capsulewebhook.Handler
}

func FreezedWebhook(handler capsulewebhook.Handler) capsulewebhook.Webhook {
	return &freezedWebhook{
		handler: handler,
	}
}

func (w *freezedWebhook) GetHandler() capsulewebhook.Handler {
	return w.handler
}

func (w *freezedWebhook) GetName() string {
	return "NamespaceFreezed"
}

func (w *freezedWebhook) GetPath() string {
	return "/validate-v1-namespace-freezed"
}

type freezedHandler struct {
	configuration configuration.Configuration
}

func FreezeHandler(configuration configuration.Configuration) capsulewebhook.Handler {
	return &freezedHandler{configuration: configuration}
}

func (r *freezedHandler) OnCreate(client client.Client, decoder *admission.Decoder, recorder record.EventRecorder) capsulewebhook.Func {
	return func(ctx context.Context, req admission.Request) admission.Response {
		ns := &corev1.Namespace{}
		if err := decoder.Decode(req, ns); err != nil {
			return admission.Errored(http.StatusBadRequest, err)
		}

		for _, objectRef := range ns.ObjectMeta.OwnerReferences {
			// retrieving the selected Tenant
			tnt := &capsulev1alpha1.Tenant{}
			if err := client.Get(ctx, types.NamespacedName{Name: objectRef.Name}, tnt); err != nil {
				return admission.Errored(http.StatusBadRequest, err)
			}

			if tnt.IsCordoned() {
				recorder.Eventf(tnt, corev1.EventTypeWarning, "TenantFreezed", "Namespace %s cannot be attached, the current Tenant is freezed", ns.GetName())

				return admission.Denied("the selected Tenant is freezed")
			}
		}
		// creating NS that is not bounded to any Tenant
		return admission.Allowed("")
	}
}

func (r *freezedHandler) OnDelete(c client.Client, decoder *admission.Decoder, recorder record.EventRecorder) capsulewebhook.Func {
	return func(ctx context.Context, req admission.Request) admission.Response {
		tntList := &capsulev1alpha1.TenantList{}
		if err := c.List(ctx, tntList, client.MatchingFieldsSelector{
			Selector: fields.OneTermEqualSelector(".status.namespaces", req.Name),
		}); err != nil {
			return admission.Errored(http.StatusBadRequest, err)
		}

		if len(tntList.Items) == 0 {
			return admission.Allowed("")
		}

		tnt := tntList.Items[0]

		if tnt.IsCordoned() && utils.RequestFromOwnerOrSA(tnt, req, r.configuration.UserGroups()) {
			recorder.Eventf(&tnt, corev1.EventTypeWarning, "TenantFreezed", "Namespaced %s cannot be deleted, the current Tenant is freezed", req.Name)

			return admission.Denied("the selected Tenant is freezed")
		}

		return admission.Allowed("")
	}
}

func (r *freezedHandler) OnUpdate(c client.Client, decoder *admission.Decoder, recorder record.EventRecorder) capsulewebhook.Func {
	return func(ctx context.Context, req admission.Request) admission.Response {
		ns := &corev1.Namespace{}
		if err := decoder.Decode(req, ns); err != nil {
			return admission.Errored(http.StatusBadRequest, err)
		}

		tntList := &capsulev1alpha1.TenantList{}
		if err := c.List(ctx, tntList, client.MatchingFieldsSelector{
			Selector: fields.OneTermEqualSelector(".status.namespaces", ns.Name),
		}); err != nil {
			return admission.Errored(http.StatusBadRequest, err)
		}

		if len(tntList.Items) == 0 {
			return admission.Allowed("")
		}

		tnt := tntList.Items[0]

		if tnt.IsCordoned() && utils.RequestFromOwnerOrSA(tnt, req, r.configuration.UserGroups()) {
			recorder.Eventf(&tnt, corev1.EventTypeWarning, "TenantFreezed", "Namespaced %s cannot be updated, the current Tenant is freezed", ns.GetName())

			return admission.Denied("the selected Tenant is freezed")
		}

		return admission.Allowed("")
	}
}
