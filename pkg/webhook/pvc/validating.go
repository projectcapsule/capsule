// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package pvc

import (
	"context"
	"net/http"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/fields"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulev1alpha1 "github.com/clastix/capsule/api/v1alpha1"
	capsulewebhook "github.com/clastix/capsule/pkg/webhook"
)

// +kubebuilder:webhook:path=/validating-v1-pvc,mutating=false,sideEffects=None,admissionReviewVersions=v1,failurePolicy=fail,groups="",resources=persistentvolumeclaims,verbs=create,versions=v1,name=pvc.capsule.clastix.io

type webhook struct {
	handler capsulewebhook.Handler
}

func Webhook(handler capsulewebhook.Handler) capsulewebhook.Webhook {
	return &webhook{handler: handler}
}

func (w *webhook) GetName() string {
	return "Pvc"
}

func (w *webhook) GetPath() string {
	return "/validating-v1-pvc"
}

func (w *webhook) GetHandler() capsulewebhook.Handler {
	return w.handler
}

type handler struct {
}

func Handler() capsulewebhook.Handler {
	return &handler{}
}

func (h *handler) OnCreate(c client.Client, decoder *admission.Decoder) capsulewebhook.Func {
	return func(ctx context.Context, req admission.Request) admission.Response {
		var valid, matched bool
		pvc := &v1.PersistentVolumeClaim{}

		if err := decoder.Decode(req, pvc); err != nil {
			return admission.Errored(http.StatusBadRequest, err)
		}

		tntList := &capsulev1alpha1.TenantList{}
		if err := c.List(ctx, tntList, client.MatchingFieldsSelector{
			Selector: fields.OneTermEqualSelector(".status.namespaces", pvc.Namespace),
		}); err != nil {
			return admission.Errored(http.StatusBadRequest, err)
		}

		if len(tntList.Items) == 0 {
			return admission.Allowed("")
		}

		tnt := tntList.Items[0]

		if tnt.Spec.StorageClasses == nil {
			return admission.Allowed("")
		}

		if pvc.Spec.StorageClassName == nil {
			return admission.Errored(http.StatusBadRequest, NewStorageClassNotValid(*tntList.Items[0].Spec.StorageClasses))
		}

		sc := *pvc.Spec.StorageClassName
		valid = tnt.Spec.StorageClasses.ExactMatch(sc)
		matched = tnt.Spec.StorageClasses.RegexMatch(sc)
		if !valid && !matched {
			return admission.Errored(http.StatusBadRequest, NewStorageClassForbidden(*pvc.Spec.StorageClassName, *tnt.Spec.StorageClasses))
		}
		return admission.Allowed("")
	}
}

func (h *handler) OnDelete(client client.Client, decoder *admission.Decoder) capsulewebhook.Func {
	return func(ctx context.Context, req admission.Request) admission.Response {
		return admission.Allowed("")
	}
}

func (h *handler) OnUpdate(client client.Client, decoder *admission.Decoder) capsulewebhook.Func {
	return func(ctx context.Context, req admission.Request) admission.Response {
		return admission.Allowed("")
	}
}
