// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package namespace

import (
	"context"
	"net/http"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulev1alpha1 "github.com/clastix/capsule/api/v1alpha1"
	capsulewebhook "github.com/clastix/capsule/pkg/webhook"
)

// +kubebuilder:webhook:path=/validate-v1-namespace-quota,mutating=false,sideEffects=None,admissionReviewVersions=v1,failurePolicy=fail,groups="",resources=namespaces,verbs=create,versions=v1,name=quota.namespace.capsule.clastix.io

type quotaWebhook struct {
	handler capsulewebhook.Handler
}

func QuotaWebhook(handler capsulewebhook.Handler) capsulewebhook.Webhook {
	return &quotaWebhook{
		handler: handler,
	}
}

func (w *quotaWebhook) GetHandler() capsulewebhook.Handler {
	return w.handler
}

func (w *quotaWebhook) GetName() string {
	return "NamespaceQuota"
}

func (w *quotaWebhook) GetPath() string {
	return "/validate-v1-namespace-quota"
}

type quotaHandler struct {
}

func QuotaHandler() capsulewebhook.Handler {
	return &quotaHandler{}
}

func (r *quotaHandler) OnCreate(client client.Client, decoder *admission.Decoder, recorder record.EventRecorder) capsulewebhook.Func {
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

			switch {
			case tnt.IsFull():
				recorder.Eventf(tnt, corev1.EventTypeWarning, "NamespaceQuotaExceded", "Namespace %s cannot be attached, quota exceeded for the current Tenant", ns.GetName())

				return admission.Denied(NewNamespaceQuotaExceededError().Error())
			case tnt.IsCordoned():
				recorder.Eventf(tnt, corev1.EventTypeWarning, "TenantFreezed", "Namespace %s cannot be attached, the current Tenant is freezed", ns.GetName())

				return admission.Denied("the selected Tenant is freezed")
			}
		}
		// creating NS that is not bounded to any Tenant
		return admission.Allowed("")
	}
}

func (r *quotaHandler) OnDelete(client client.Client, decoder *admission.Decoder, recorder record.EventRecorder) capsulewebhook.Func {
	return func(ctx context.Context, req admission.Request) admission.Response {
		return admission.Allowed("")
	}
}

func (r *quotaHandler) OnUpdate(client client.Client, decoder *admission.Decoder, recorder record.EventRecorder) capsulewebhook.Func {
	return func(ctx context.Context, req admission.Request) admission.Response {
		return admission.Allowed("")
	}
}
