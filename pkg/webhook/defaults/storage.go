// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package defaults

import (
	"context"
	"encoding/json"

	corev1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulev1beta2 "github.com/clastix/capsule/api/v1beta2"
	"github.com/clastix/capsule/pkg/webhook/utils"
)

func mutatePVCDefaults(ctx context.Context, req admission.Request, c client.Client, decoder *admission.Decoder, recorder record.EventRecorder, namespace string) *admission.Response {
	var err error

	pvc := &corev1.PersistentVolumeClaim{}
	if err = decoder.Decode(req, pvc); err != nil {
		return utils.ErroredResponse(err)
	}

	pvc.SetNamespace(namespace)

	var tnt *capsulev1beta2.Tenant

	tnt, err = utils.TenantByStatusNamespace(ctx, c, pvc.Namespace)
	if err != nil {
		return utils.ErroredResponse(err)
	}

	if tnt == nil {
		return nil
	}

	allowed := tnt.Spec.StorageClasses

	if allowed == nil || allowed.Default == "" {
		return nil
	}

	var mutate bool

	var csc *storagev1.StorageClass

	if storageClassName := pvc.Spec.StorageClassName; storageClassName != nil && *storageClassName != allowed.Default {
		csc, err = utils.GetStorageClassByName(ctx, c, *storageClassName)
		if err != nil && !k8serrors.IsNotFound(err) {
			response := admission.Denied(NewStorageClassError(*storageClassName, err).Error())

			return &response
		}
	} else {
		mutate = true
	}

	if mutate = mutate || (utils.IsDefaultStorageClass(csc) && csc.GetName() != allowed.Default); !mutate {
		return nil
	}

	pvc.Spec.StorageClassName = &tnt.Spec.StorageClasses.Default
	// Marshal Manifest
	marshaled, err := json.Marshal(pvc)
	if err != nil {
		return utils.ErroredResponse(err)
	}

	recorder.Eventf(tnt, corev1.EventTypeNormal, "TenantDefault", "Assigned Tenant default Storage Class %s to %s/%s", allowed.Default, pvc.Namespace, pvc.Name)

	response := admission.PatchResponseFromRaw(req.Object.Raw, marshaled)

	return &response
}
