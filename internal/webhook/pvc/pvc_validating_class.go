// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package pvc

import (
	"context"
	"net/http"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/internal/webhook/utils"
	"github.com/projectcapsule/capsule/pkg/api/errors"
	ad "github.com/projectcapsule/capsule/pkg/runtime/admission"
	"github.com/projectcapsule/capsule/pkg/runtime/events"
	"github.com/projectcapsule/capsule/pkg/runtime/handlers"
)

type persistentVolumeValidatingClass struct{}

func PersistentVolumeValidatingClass() handlers.TypedHandlerWithTenant[*corev1.PersistentVolumeClaim] {
	return &persistentVolumeValidatingClass{}
}

func (h *persistentVolumeValidatingClass) OnCreate(
	_ client.Client,
	reader client.Reader,
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
			recorder.LabeledEvent(
				pvc,
				corev1.EventTypeWarning,
				events.ReasonMissingStorageClass,
				events.ActionValidationDenied,
				"persistentvolume must provide a storageclass",
			).
				WithRelated(tnt).
				WithTenantLabel(tnt).
				WithRequestAnnotations(req).
				Emit(ctx)

			return ad.Deny(errors.NewStorageClassNotValid(*tnt.Spec.StorageClasses).Error())
		}

		selector := false

		// Verify if the StorageClass exists and matches the label selector/expression
		if len(allowed.MatchExpressions) > 0 || len(allowed.MatchLabels) > 0 {
			storageClassObj, err := utils.GetStorageClassByName(ctx, reader, *storageClass)
			if err != nil && !apierrors.IsNotFound(err) {
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
				events.ReasonForbiddenStorageClass,
				events.ActionValidationDenied,
				"StorageClass %s is forbidden for the Tenant %s", *storageClass, tnt.GetName())

			return ad.Deny(errors.NewStorageClassForbidden(*pvc.Spec.StorageClassName, *tnt.Spec.StorageClasses).Error())
		}
	}
}

func (h *persistentVolumeValidatingClass) OnUpdate(
	client.Client,
	client.Reader,
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

func (h *persistentVolumeValidatingClass) OnDelete(
	client.Client,
	client.Reader,
	*corev1.PersistentVolumeClaim,
	admission.Decoder,
	events.EventRecorder,
	*capsulev1beta2.Tenant,
) handlers.Func {
	return func(context.Context, admission.Request) *admission.Response {
		return nil
	}
}
