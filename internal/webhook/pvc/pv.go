// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package pvc

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/events"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/internal/webhook/utils"
	caperrors "github.com/projectcapsule/capsule/pkg/api/errors"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	evt "github.com/projectcapsule/capsule/pkg/runtime/events"
	"github.com/projectcapsule/capsule/pkg/runtime/handlers"
)

type pv struct{}

func PersistentVolumeReuse() handlers.TypedHandlerWithTenant[*corev1.PersistentVolumeClaim] {
	return &pv{}
}

func (h pv) OnCreate(
	c client.Client,
	pvc *corev1.PersistentVolumeClaim,
	decoder admission.Decoder,
	recorder events.EventRecorder,
	tnt *capsulev1beta2.Tenant,
) handlers.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		pvObj, err := h.handle(ctx, c, pvc, tnt.Name)
		if err == nil {
			return nil
		}

		var related runtime.Object
		if pvObj != nil {
			related = pvObj
		} else {
			related = tnt
		}

		caperrors.RecordTypedErrorEvent(recorder, pvc, related, err)

		return utils.ErroredResponse(err)
	}
}

func (h pv) OnUpdate(
	client.Client,
	*corev1.PersistentVolumeClaim,
	*corev1.PersistentVolumeClaim,
	admission.Decoder,
	events.EventRecorder,
	*capsulev1beta2.Tenant,
) handlers.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		return nil
	}
}

func (h pv) OnDelete(
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

func (h pv) handle(
	ctx context.Context,
	c client.Client,
	pvc *corev1.PersistentVolumeClaim,
	tenantName string,
) (*corev1.PersistentVolume, error) {
	if pvc.Spec.Selector != nil {
		return nil, caperrors.NewPVSelectorError(evt.ActionValidationDenied)
	}

	if pvc.Spec.VolumeName == "" {
		return nil, nil
	}

	pv := &corev1.PersistentVolume{}
	if err := c.Get(ctx, types.NamespacedName{Name: pvc.Spec.VolumeName}, pv); err != nil {
		if errors.IsNotFound(err) {
			return nil, caperrors.NewPvNotFoundError(
				pvc.Spec.VolumeName,
				evt.ActionValidationDenied,
			)
		}

		return nil, err
	}

	labels := pv.GetLabels()

	value, ok := labels[meta.TenantLabel]
	if !ok {
		return pv, caperrors.NewMissingTenantPVLabelsError(
			pv.GetName(),
			evt.ActionValidationDenied,
		)
	}

	if value != tenantName {
		return pv, caperrors.NewCrossTenantPVMountError(pv.GetName(), evt.ActionValidationDenied)
	}

	return pv, nil
}
