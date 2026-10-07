// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package pvc

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/errors"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	ad "github.com/projectcapsule/capsule/pkg/runtime/admission"
	"github.com/projectcapsule/capsule/pkg/runtime/events"
	"github.com/projectcapsule/capsule/pkg/runtime/handlers"
)

// VolumeAccess evaluates additional permissions only after legacy ownership
// validation finds a PV without a tenant label. It must never mutate the PV.
type VolumeAccess interface {
	Allows(ctx context.Context, c client.Client, reader client.Reader, req admission.Request, claim *corev1.PersistentVolumeClaim, volume *corev1.PersistentVolume, tnt *capsulev1beta2.Tenant, recorder events.EventRecorder) (bool, error)
}

type persistentVolumeValidatingVolume struct {
	access VolumeAccess
}

func PersistentVolumeValidatingVolume(access VolumeAccess) handlers.TypedHandlerWithTenant[*corev1.PersistentVolumeClaim] {
	return &persistentVolumeValidatingVolume{access: access}
}

func (h persistentVolumeValidatingVolume) OnCreate(
	c client.Client,
	reader client.Reader,
	pvc *corev1.PersistentVolumeClaim,
	decoder admission.Decoder,
	recorder events.EventRecorder,
	tnt *capsulev1beta2.Tenant,
) handlers.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		if err := validatePVCSelector(pvc, tnt); err != nil {
			return ad.ErroredResponse(err)
		}

		return validatePVCVolumeName(ctx, reader, pvc, tnt, func(pv *corev1.PersistentVolume) (bool, error) {
			if h.access == nil {
				return false, nil
			}

			return h.access.Allows(ctx, c, reader, req, pvc, pv, tnt, recorder)
		})
	}
}

func (h persistentVolumeValidatingVolume) OnUpdate(
	c client.Client,
	reader client.Reader,
	oldPVC *corev1.PersistentVolumeClaim,
	newPVC *corev1.PersistentVolumeClaim,
	decoder admission.Decoder,
	recorder events.EventRecorder,
	tnt *capsulev1beta2.Tenant,
) handlers.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		if isBoundPVC(oldPVC) {
			return nil
		}

		if err := validatePVCSelector(newPVC, tnt); err != nil {
			return ad.ErroredResponse(err)
		}

		return validatePVCVolumeName(ctx, reader, newPVC, tnt, func(pv *corev1.PersistentVolume) (bool, error) {
			if h.access == nil {
				return false, nil
			}

			return h.access.Allows(ctx, c, reader, req, newPVC, pv, tnt, recorder)
		})
	}
}

func (h persistentVolumeValidatingVolume) OnDelete(
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
	c client.Reader,
	pvc *corev1.PersistentVolumeClaim,
	tnt *capsulev1beta2.Tenant,
	additionalAccess func(*corev1.PersistentVolume) (bool, error),
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
		if apierrors.IsNotFound(err) {
			err = fmt.Errorf("cannot create a PVC referring to a not yet existing PV")
		}

		return ad.ErroredResponse(err)
	}

	value, ok := pv.GetLabels()[meta.TenantLabel]
	if ok {
		if value != tnt.Name {
			return ad.Deny(errors.NewCrossTenantPVMountError(pv.GetName(), events.ActionValidationDenied).Error())
		}

		return nil
	}

	if additionalAccess != nil && pv.DeletionTimestamp == nil {
		allowed, err := additionalAccess(&pv)
		if err != nil {
			return ad.ErroredResponse(err)
		}

		if allowed {
			return nil
		}
	}

	return ad.Deny(errors.NewMissingTenantPVLabelsError(pv.GetName(), events.ActionValidationDenied).Error())
}
