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
			oldPVC *corev1.PersistentVolumeClaim,
		) bool {
			if !requiresPVCSpecValidation(req, pvc, oldPVC) {
				return false
			}

			if pvc.Spec.Selector != nil {
				return true
			}

			return req.Operation == admissionv1.Create && pvc.Spec.VolumeName != ""
		},
	}
}

func requiresPVCSpecValidation(
	req admission.Request,
	pvc *corev1.PersistentVolumeClaim,
	oldPVC *corev1.PersistentVolumeClaim,
) bool {
	// A bound PVC's volume binding fields, including its selector, are immutable.
	// Reapplying the tenant selector during an update would make otherwise valid
	// metadata or resize updates fail for claims created before Capsule enforced it.
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
