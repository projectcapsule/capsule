// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package pvc

import (
	"context"
	"net/http"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulewebhook "github.com/clastix/capsule/pkg/webhook"
	"github.com/clastix/capsule/pkg/webhook/utils"
)

type validating struct{}

func Validating() capsulewebhook.Handler {
	return &validating{}
}

func (h *validating) OnCreate(c client.Client, decoder *admission.Decoder, recorder record.EventRecorder) capsulewebhook.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		pvc := &corev1.PersistentVolumeClaim{}
		if err := decoder.Decode(req, pvc); err != nil {
			return utils.ErroredResponse(err)
		}

		tnt, err := utils.TenantByStatusNamespace(ctx, c, pvc.Namespace)
		if err != nil {
			return utils.ErroredResponse(err)
		}

		if tnt == nil {
			return nil
		}

		allowed := tnt.Spec.StorageClasses

		if allowed == nil {
			return nil
		}

		storageClass := pvc.Spec.StorageClassName

		if storageClass == nil {
			recorder.Eventf(tnt, corev1.EventTypeWarning, "MissingStorageClass", "PersistentVolumeClaim %s/%s is missing StorageClass", req.Namespace, req.Name)

			response := admission.Denied(NewStorageClassNotValid(*tnt.Spec.StorageClasses).Error())

			return &response
		}

		selector := false

		// Verify if the StorageClass exists and matches the label selector/expression
		if len(allowed.MatchExpressions) > 0 || len(allowed.MatchLabels) > 0 {
			storageClassObj, err := utils.GetStorageClassByName(ctx, c, *storageClass)
			if err != nil && !errors.IsNotFound(err) {
				response := admission.Errored(http.StatusInternalServerError, err)

				return &response
			}

			// Storage Class is present, check if it matches the selector
			if storageClassObj != nil {
				selector = allowed.SelectorMatch(storageClassObj)
			}
		}

		switch {
		case allowed.MatchDefault(*storageClass):
			return nil
		case allowed.Match(*storageClass) || selector:
			return nil
		default:
			recorder.Eventf(tnt, corev1.EventTypeWarning, "ForbiddenStorageClass", "PersistentVolumeClaim %s/%s StorageClass %s is forbidden for the current Tenant", req.Namespace, req.Name, *storageClass)

			response := admission.Denied(NewStorageClassForbidden(*pvc.Spec.StorageClassName, *tnt.Spec.StorageClasses).Error())

			return &response
		}
	}
}

func (h *validating) OnDelete(client.Client, *admission.Decoder, record.EventRecorder) capsulewebhook.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		return nil
	}
}

func (h *validating) OnUpdate(client.Client, *admission.Decoder, record.EventRecorder) capsulewebhook.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		return nil
	}
}
