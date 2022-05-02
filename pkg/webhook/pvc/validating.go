// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package pvc

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulev1beta1 "github.com/clastix/capsule/api/v1beta1"
	capsulewebhook "github.com/clastix/capsule/pkg/webhook"
	"github.com/clastix/capsule/pkg/webhook/utils"
)

type handler struct{}

func Handler() capsulewebhook.Handler {
	return &handler{}
}

func (h *handler) OnCreate(c client.Client, decoder *admission.Decoder, recorder record.EventRecorder) capsulewebhook.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		var valid, matched bool

		pvc := &corev1.PersistentVolumeClaim{}
		if err := decoder.Decode(req, pvc); err != nil {
			return utils.ErroredResponse(err)
		}

		tntList := &capsulev1beta1.TenantList{}
		if err := c.List(ctx, tntList, client.MatchingFieldsSelector{
			Selector: fields.OneTermEqualSelector(".status.namespaces", pvc.Namespace),
		}); err != nil {
			return utils.ErroredResponse(err)
		}

		if len(tntList.Items) == 0 {
			return nil
		}

		tnt := tntList.Items[0]

		if tnt.Spec.StorageClasses == nil {
			return nil
		}

		if pvc.Spec.StorageClassName == nil {
			recorder.Eventf(&tnt, corev1.EventTypeWarning, "MissingStorageClass", "PersistentVolumeClaim %s/%s is missing StorageClass", req.Namespace, req.Name)

			response := admission.Denied(NewStorageClassNotValid(*tntList.Items[0].Spec.StorageClasses).Error())

			return &response
		}

		sc := *pvc.Spec.StorageClassName
		if valid, matched = tnt.Spec.StorageClasses.ExactMatch(sc), tnt.Spec.StorageClasses.RegexMatch(sc); !valid && !matched {
			recorder.Eventf(&tnt, corev1.EventTypeWarning, "ForbiddenStorageClass", "PersistentVolumeClaim %s/%s StorageClass %s is forbidden for the current Tenant", req.Namespace, req.Name, sc)

			response := admission.Denied(NewStorageClassForbidden(*pvc.Spec.StorageClassName, *tnt.Spec.StorageClasses).Error())

			return &response
		}

		return nil
	}
}

func (h *handler) OnDelete(client.Client, *admission.Decoder, record.EventRecorder) capsulewebhook.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		return nil
	}
}

func (h *handler) OnUpdate(client.Client, *admission.Decoder, record.EventRecorder) capsulewebhook.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		return nil
	}
}
