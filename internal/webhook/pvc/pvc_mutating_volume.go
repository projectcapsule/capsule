// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package pvc

import (
	"context"
	"encoding/json"
	"net/http"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	capsulewebhook "github.com/projectcapsule/capsule/internal/webhook"
	"github.com/projectcapsule/capsule/pkg/api/meta"
)

type persistentVolumeMutatingVolume struct{}

func PersistentVolumeMutatingVolume() capsulewebhook.TypedHandlerWithTenant[*corev1.PersistentVolumeClaim] {
	return &persistentVolumeMutatingVolume{}
}

func (h persistentVolumeMutatingVolume) OnCreate(
	c client.Client,
	pvc *corev1.PersistentVolumeClaim,
	decoder admission.Decoder,
	recorder record.EventRecorder,
	tnt *capsulev1beta2.Tenant,
) capsulewebhook.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		// Kubernetes does not dynamically provision PVs for PVCs with a non-empty selector.
		// Therefore, only mutate PVCs that already opted into static binding semantics:
		// - either by setting spec.selector
		// - or by pre-binding through spec.volumeName
		if pvc.Spec.Selector == nil && pvc.Spec.VolumeName == "" {
			return nil
		}

		pvc.Spec.Selector = addTenantSelectorExpression(pvc.Spec.Selector, tnt.Name)

		marshaled, err := json.Marshal(pvc)
		if err != nil {
			response := admission.Errored(http.StatusInternalServerError, err)

			return &response
		}

		response := admission.PatchResponseFromRaw(req.Object.Raw, marshaled)

		return &response
	}
}

func (h persistentVolumeMutatingVolume) OnUpdate(
	c client.Client,
	oldPVC *corev1.PersistentVolumeClaim,
	newPVC *corev1.PersistentVolumeClaim,
	decoder admission.Decoder,
	recorder record.EventRecorder,
	tnt *capsulev1beta2.Tenant,
) capsulewebhook.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		if newPVC == nil || tnt == nil {
			return nil
		}

		// Avoid mutating normal dynamically provisioned PVCs.
		//
		// Only canonicalize tenant selector if the PVC already participates in
		// static binding semantics.
		if newPVC.Spec.Selector == nil {
			return nil
		}

		newPVC.Spec.Selector = addTenantSelectorExpression(newPVC.Spec.Selector, tnt.Name)

		marshaled, err := json.Marshal(newPVC)
		if err != nil {
			response := admission.Errored(http.StatusInternalServerError, err)

			return &response
		}

		response := admission.PatchResponseFromRaw(req.Object.Raw, marshaled)

		return &response
	}
}

func (h persistentVolumeMutatingVolume) OnDelete(
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
func addTenantSelectorExpression(
	selector *metav1.LabelSelector,
	tenantName string,
) *metav1.LabelSelector {
	if selector == nil {
		selector = &metav1.LabelSelector{}
	}

	// Remove tenant label from MatchLabels to avoid conflicting requirements.
	if selector.MatchLabels != nil {
		delete(selector.MatchLabels, meta.TenantLabel)

		if len(selector.MatchLabels) == 0 {
			selector.MatchLabels = nil
		}
	}

	// Remove any existing tenant expression, regardless of operator or value.
	matchExpressions := make([]metav1.LabelSelectorRequirement, 0, len(selector.MatchExpressions))
	for _, expression := range selector.MatchExpressions {
		if expression.Key == meta.TenantLabel {
			continue
		}

		matchExpressions = append(matchExpressions, expression)
	}

	selector.MatchExpressions = append(matchExpressions, metav1.LabelSelectorRequirement{
		Key:      meta.TenantLabel,
		Operator: metav1.LabelSelectorOpIn,
		Values:   []string{tenantName},
	})

	return selector
}
