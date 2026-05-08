// Copyright 2020-2025 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package pvc

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	capsulewebhook "github.com/projectcapsule/capsule/internal/webhook"
	"github.com/projectcapsule/capsule/internal/webhook/utils"
	"github.com/projectcapsule/capsule/pkg/api/meta"
)

type persistentVolumeValidatingVolume struct{}

func PersistentVolumeValidatingVolume() capsulewebhook.TypedHandlerWithTenant[*corev1.PersistentVolumeClaim] {
	return &persistentVolumeValidatingVolume{}
}

func (h persistentVolumeValidatingVolume) OnCreate(
	c client.Client,
	pvc *corev1.PersistentVolumeClaim,
	decoder admission.Decoder,
	recorder record.EventRecorder,
	tnt *capsulev1beta2.Tenant,
) capsulewebhook.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		if err := validatePVCSelector(pvc, tnt); err != nil {
			return utils.ErroredResponse(err)
		}

		return validatePVCVolumeName(ctx, c, pvc, tnt)
	}
}

func (h persistentVolumeValidatingVolume) OnUpdate(
	c client.Client,
	oldPVC *corev1.PersistentVolumeClaim,
	newPVC *corev1.PersistentVolumeClaim,
	decoder admission.Decoder,
	recorder record.EventRecorder,
	tnt *capsulev1beta2.Tenant,
) capsulewebhook.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		if err := validatePVCSelector(newPVC, tnt); err != nil {
			return utils.ErroredResponse(err)
		}

		return validatePVCVolumeName(ctx, c, newPVC, tnt)
	}
}

func (h persistentVolumeValidatingVolume) OnDelete(
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

func validatePVCSelector(
	pvc *corev1.PersistentVolumeClaim,
	tnt *capsulev1beta2.Tenant,
) error {
	if pvc == nil || tnt == nil || pvc.Spec.Selector == nil {
		return nil
	}

	for _, expression := range pvc.Spec.Selector.MatchExpressions {
		if expression.Key != meta.TenantLabel {
			continue
		}

		if expression.Operator != metav1.LabelSelectorOpIn {
			return fmt.Errorf(
				"PVC selector expression for %q must use operator %q",
				meta.TenantLabel,
				metav1.LabelSelectorOpIn,
			)
		}

		if len(expression.Values) != 1 || expression.Values[0] != tnt.Name {
			return fmt.Errorf(
				"PVC selector expression for %q must contain only tenant %q",
				meta.TenantLabel,
				tnt.Name,
			)
		}

		return nil
	}

	return fmt.Errorf(
		"PVC selector must include tenant selector expression %q In [%q]",
		meta.TenantLabel,
		tnt.Name,
	)
}

func validatePVCVolumeName(
	ctx context.Context,
	c client.Client,
	pvc *corev1.PersistentVolumeClaim,
	tnt *capsulev1beta2.Tenant,
) *admission.Response {
	if pvc == nil || tnt == nil {
		return nil
	}

	// The PVC hasn't any volumeName pre-claimed, it can be skipped.
	if pvc.Spec.VolumeName == "" {
		return nil
	}

	// Checking if the PV is labelled with the Tenant name.
	pv := corev1.PersistentVolume{}
	if err := c.Get(ctx, types.NamespacedName{Name: pvc.Spec.VolumeName}, &pv); err != nil {
		if errors.IsNotFound(err) {
			err = fmt.Errorf("cannot create a PVC referring to a not yet existing PV")
		}

		return utils.ErroredResponse(err)
	}

	if pv.GetLabels() == nil {
		return utils.ErroredResponse(NewMissingPVLabelsError(pv.GetName()))
	}

	value, ok := pv.GetLabels()[meta.TenantLabel]
	if !ok {
		return utils.ErroredResponse(NewMissingTenantPVLabelsError(pv.GetName()))
	}

	if value != tnt.Name {
		return utils.ErroredResponse(NewCrossTenantPVMountError(pv.GetName()))
	}

	return nil
}
