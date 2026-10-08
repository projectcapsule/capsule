// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package pvc

import (
	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	apiequality "k8s.io/apimachinery/pkg/api/equality"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	"github.com/projectcapsule/capsule/pkg/runtime/handlers"
)

func Handler(handler ...handlers.TypedHandlerWithTenant[*corev1.PersistentVolumeClaim]) handlers.Handler {
	return &handlers.TypedTenantHandler[*corev1.PersistentVolumeClaim]{
		Factory: func() *corev1.PersistentVolumeClaim {
			return &corev1.PersistentVolumeClaim{}
		},
		Handlers:  handler,
		Predicate: requiresPVCSpecValidation,
	}
}

func MutatingHandler(handler ...handlers.TypedHandlerWithTenant[*corev1.PersistentVolumeClaim]) handlers.Handler {
	return &handlers.TypedTenantHandler[*corev1.PersistentVolumeClaim]{
		Factory: func() *corev1.PersistentVolumeClaim {
			return &corev1.PersistentVolumeClaim{}
		},
		Handlers: handler,
		Predicate: func(
			req admission.Request,
			pvc *corev1.PersistentVolumeClaim,
			_ *corev1.PersistentVolumeClaim,
		) bool {
			// Selectors are immutable after creation, including while Pending.
			return req.Operation == admissionv1.Create && pvc != nil &&
				(pvc.Spec.Selector != nil || pvc.Spec.VolumeName != "")
		},
	}
}

func requiresPVCSpecValidation(
	req admission.Request,
	pvc *corev1.PersistentVolumeClaim,
	oldPVC *corev1.PersistentVolumeClaim,
) bool {
	// A bound PVC's volume binding fields are immutable. Metadata and resize
	// updates do not introduce a new volume binding to validate.
	if req.Operation == admissionv1.Update &&
		isBoundPVC(oldPVC) {
		return false
	}

	// Finalizer cleanup must remain possible after a bound PV has disappeared.
	// Continue validating any update that changes the PVC spec.
	if req.Operation != admissionv1.Update ||
		pvc == nil ||
		oldPVC == nil ||
		pvc.DeletionTimestamp == nil {
		return true
	}

	return !apiequality.Semantic.DeepEqual(pvc.Spec, oldPVC.Spec)
}

func isBoundPVC(pvc *corev1.PersistentVolumeClaim) bool {
	return pvc != nil && pvc.Status.Phase == corev1.ClaimBound
}
