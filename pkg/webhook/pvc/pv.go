// Copyright 2020-2021 Clastix Labs
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

	capsulev1beta2 "github.com/clastix/capsule/api/v1beta2"
	capsulewebhook "github.com/clastix/capsule/pkg/webhook"
	"github.com/clastix/capsule/pkg/webhook/utils"
)

type PV struct {
	capsuleLabel string
}

func PersistentVolumeReuse() capsulewebhook.Handler {
	value, err := capsulev1beta2.GetTypeLabel(&capsulev1beta2.Tenant{})
	if err != nil {
		panic(fmt.Sprintf("this shouldn't happen: %s", err.Error()))
	}

	return &PV{
		capsuleLabel: value,
	}
}

func (p PV) OnCreate(client client.Client, decoder *admission.Decoder, recorder record.EventRecorder) capsulewebhook.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		pvc := corev1.PersistentVolumeClaim{}
		if err := decoder.Decode(req, &pvc); err != nil {
			return utils.ErroredResponse(err)
		}

		tnt, err := utils.TenantByStatusNamespace(ctx, client, pvc.GetNamespace())
		if err != nil {
			return utils.ErroredResponse(err)
		}
		// PVC is not in a Tenant Namespace, skipping
		if tnt == nil {
			return nil
		}
		// A PersistentVolume selector cannot help in preventing a cross-tenant mount:
		// thus, disallowing that in first place.
		if pvc.Spec.Selector != nil {
			return utils.ErroredResponse(NewPVSelectorError())
		}
		// The PVC hasn't any volumeName pre-claimed, it can be skipped
		if len(pvc.Spec.VolumeName) == 0 {
			return nil
		}
		// Checking if the PV is labelled with the Tenant name
		pv := corev1.PersistentVolume{}
		if err = client.Get(ctx, types.NamespacedName{Name: pvc.Spec.VolumeName}, &pv); err != nil {
			if errors.IsNotFound(err) {
				err = fmt.Errorf("cannot create a PVC referring to a not yet existing PV")
			}

			return utils.ErroredResponse(err)
		}

		if pv.GetLabels() == nil {
			return utils.ErroredResponse(NewMissingPVLabelsError(pv.GetName()))
		}

		value, ok := pv.GetLabels()[p.capsuleLabel]
		if !ok {
			return utils.ErroredResponse(NewMissingTenantPVLabelsError(pv.GetName()))
		}

		if value != tnt.Name {
			return utils.ErroredResponse(NewCrossTenantPVMountError(pv.GetName()))
		}

		return nil
	}
}

func (p PV) OnDelete(client.Client, *admission.Decoder, record.EventRecorder) capsulewebhook.Func {
	return func(context.Context, admission.Request) *admission.Response {
		return nil
	}
}

func (p PV) OnUpdate(client.Client, *admission.Decoder, record.EventRecorder) capsulewebhook.Func {
	return func(context.Context, admission.Request) *admission.Response {
		return nil
	}
}
