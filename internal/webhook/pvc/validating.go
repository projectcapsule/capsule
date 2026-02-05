// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package pvc

import (
	"context"
	"net/http"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/client-go/tools/events"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/internal/webhook/utils"
	caperrors "github.com/projectcapsule/capsule/pkg/api/errors"
	evt "github.com/projectcapsule/capsule/pkg/runtime/events"
	"github.com/projectcapsule/capsule/pkg/runtime/handlers"
)

type validating struct{}

func Validating() handlers.TypedHandlerWithTenant[*corev1.PersistentVolumeClaim] {
	return &validating{}
}

func (h *validating) OnCreate(
	c client.Client,
	pvc *corev1.PersistentVolumeClaim,
	decoder admission.Decoder,
	recorder events.EventRecorder,
	tnt *capsulev1beta2.Tenant,
) handlers.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		allowed := tnt.Spec.StorageClasses

		if allowed == nil {
			return nil
		}

		storageClass := pvc.Spec.StorageClassName

		if storageClass == nil {
			recorder.Eventf(
				pvc,
				&tnt,
				corev1.EventTypeWarning,
				evt.ReasonMissingStorageClass,
				evt.ActionValidationDenied,
				"Requires a StorageClass",
			)

			response := admission.Denied(caperrors.NewStorageClassNotValid(*tnt.Spec.StorageClasses).Error())

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
			recorder.Eventf(
				pvc,
				tnt,
				corev1.EventTypeWarning,
				evt.ReasonForbiddenStorageClass,
				evt.ActionValidationDenied,
				"StorageClass %s is forbidden for the Tenant %s", *storageClass, tnt.GetName())

			response := admission.Denied(caperrors.NewStorageClassForbidden(*pvc.Spec.StorageClassName, *tnt.Spec.StorageClasses).Error())

			return &response
		}
	}
}

func (h *validating) OnUpdate(
	client.Client,
	*corev1.PersistentVolumeClaim,
	*corev1.PersistentVolumeClaim,
	admission.Decoder,
	events.EventRecorder,
	*capsulev1beta2.Tenant,
) handlers.Func {
	return func(context.Context, admission.Request) *admission.Response {
		return nil
	}
}

func (h *validating) OnDelete(
	client.Client,
	*corev1.PersistentVolumeClaim,
	admission.Decoder,
	events.EventRecorder,
	*capsulev1beta2.Tenant,
) handlers.Func {
	return func(context.Context, admission.Request) *admission.Response {
		return nil
	}
}
