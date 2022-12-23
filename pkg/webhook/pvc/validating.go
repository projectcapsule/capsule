// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package pvc

import (
	"context"
	"net/http"

	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/storage/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulev1beta2 "github.com/clastix/capsule/api/v1beta2"
	capsulewebhook "github.com/clastix/capsule/pkg/webhook"
	"github.com/clastix/capsule/pkg/webhook/utils"
)

type handler struct{}

func Handler() capsulewebhook.Handler {
	return &handler{}
}

func (h *handler) getStorageClass(ctx context.Context, c client.Client, name string) (client.Object, error) {
	obj := &v1.StorageClass{}

	if err := c.Get(ctx, types.NamespacedName{Name: name}, obj); err != nil {
		if errors.IsNotFound(err) {
			return nil, nil
		}

		return nil, err
	}

	return obj, nil
}

func (h *handler) OnCreate(c client.Client, decoder *admission.Decoder, recorder record.EventRecorder) capsulewebhook.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		pvc := &corev1.PersistentVolumeClaim{}
		if err := decoder.Decode(req, pvc); err != nil {
			return utils.ErroredResponse(err)
		}

		tntList := &capsulev1beta2.TenantList{}
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

		scObj, err := h.getStorageClass(ctx, c, sc)
		if err != nil {
			response := admission.Errored(http.StatusInternalServerError, err)

			return &response
		}

		var valid, regex, match bool

		valid, regex = tnt.Spec.StorageClasses.ExactMatch(sc), tnt.Spec.StorageClasses.RegexMatch(sc)

		if scObj != nil {
			match = tnt.Spec.StorageClasses.SelectorMatch(scObj)
		} else {
			match = true
		}

		if !valid && !regex && !match {
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
