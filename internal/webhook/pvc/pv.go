// Copyright 2020-2025 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package pvc

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	capsulewebhook "github.com/projectcapsule/capsule/internal/webhook"
	"github.com/projectcapsule/capsule/internal/webhook/utils"
	caperrors "github.com/projectcapsule/capsule/pkg/api/errors"
	"github.com/projectcapsule/capsule/pkg/api/meta"
)

type pv struct{}

func PersistentVolumeReuse() capsulewebhook.TypedHandlerWithTenant[*corev1.PersistentVolumeClaim] {
	return &pv{}
}

func (h pv) OnCreate(
	c client.Client,
	pvc *corev1.PersistentVolumeClaim,
	decoder admission.Decoder,
	recorder record.EventRecorder,
	tnt *capsulev1beta2.Tenant,
) capsulewebhook.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		// A PersistentVolume selector cannot help in preventing a cross-tenant mount:
		// thus, disallowing that in first place.
		if pvc.Spec.Selector != nil {
			return utils.ErroredResponse(caperrors.NewPVSelectorError())
		}

		// The PVC hasn't any volumeName pre-claimed, it can be skipped
		if len(pvc.Spec.VolumeName) == 0 {
			return nil
		}

		// Checking if the PV is labelled with the Tenant name
		pv := corev1.PersistentVolume{}
		if err := c.Get(ctx, types.NamespacedName{Name: pvc.Spec.VolumeName}, &pv); err != nil {
			if errors.IsNotFound(err) {
				err = fmt.Errorf("cannot create a PVC referring to a not yet existing PV")
			}

			return utils.ErroredResponse(err)
		}

		if pv.GetLabels() == nil {
			return utils.ErroredResponse(caperrors.NewMissingPVLabelsError(pv.GetName()))
		}

		value, ok := pv.GetLabels()[meta.TenantLabel]
		if !ok {
			return utils.ErroredResponse(caperrors.NewMissingTenantPVLabelsError(pv.GetName()))
		}

		if value != tnt.Name {
			return utils.ErroredResponse(caperrors.NewCrossTenantPVMountError(pv.GetName()))
		}

		return nil
	}
}

func (h pv) OnUpdate(
	client.Client,
	*corev1.PersistentVolumeClaim,
	*corev1.PersistentVolumeClaim,
	admission.Decoder,
	record.EventRecorder,
	*capsulev1beta2.Tenant,
) capsulewebhook.Func {
	return func(context.Context, admission.Request) *admission.Response {
		return nil
	}
}

func (h pv) OnDelete(
	client.Client,
	*corev1.PersistentVolumeClaim,
	admission.Decoder,
	record.EventRecorder,
	*capsulev1beta2.Tenant,
) capsulewebhook.Func {
	return func(context.Context, admission.Request) *admission.Response {
		return nil
	}
}
