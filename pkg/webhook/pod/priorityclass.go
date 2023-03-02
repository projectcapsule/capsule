// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package pod

import (
	"context"
	"net/http"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulewebhook "github.com/clastix/capsule/pkg/webhook"
	"github.com/clastix/capsule/pkg/webhook/utils"
)

type priorityClass struct{}

func PriorityClass() capsulewebhook.Handler {
	return &priorityClass{}
}

func (h *priorityClass) OnCreate(c client.Client, decoder *admission.Decoder, recorder record.EventRecorder) capsulewebhook.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		pod := &corev1.Pod{}
		if err := decoder.Decode(req, pod); err != nil {
			return utils.ErroredResponse(err)
		}

		tnt, err := utils.TenantByStatusNamespace(ctx, c, pod.Namespace)
		if err != nil {
			return utils.ErroredResponse(err)
		}

		if tnt == nil {
			return nil
		}

		allowed := tnt.Spec.PriorityClasses

		if allowed == nil {
			return nil
		}

		priorityClassName := pod.Spec.PriorityClassName

		if len(priorityClassName) == 0 {
			// We don't have to force Pod to specify a Priority Class
			return nil
		}

		selector := false

		// Verify if the StorageClass exists and matches the label selector/expression
		if len(allowed.MatchExpressions) > 0 || len(allowed.MatchLabels) > 0 {
			priorityClassObj, err := utils.GetPriorityClassByName(ctx, c, priorityClassName)
			if err != nil {
				response := admission.Errored(http.StatusInternalServerError, err)

				return &response
			}

			// Storage Class is present, check if it matches the selector
			if priorityClassObj != nil {
				selector = allowed.SelectorMatch(priorityClassObj)
			}
		}

		switch {
		case allowed.MatchDefault(priorityClassName):
			// Allow if given Priority Class is equal tenant default (eventough it's not allowed by selector)
			return nil
		case allowed.Match(priorityClassName) || selector:
			return nil
		default:
			recorder.Eventf(tnt, corev1.EventTypeWarning, "ForbiddenPriorityClass", "Pod %s/%s is using Priority Class %s is forbidden for the current Tenant", pod.Namespace, pod.Name, priorityClassName)

			response := admission.Denied(NewPodPriorityClassForbidden(priorityClassName, *allowed).Error())

			return &response
		}
	}
}

func (h *priorityClass) OnDelete(client.Client, *admission.Decoder, record.EventRecorder) capsulewebhook.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		return nil
	}
}

func (h *priorityClass) OnUpdate(client.Client, *admission.Decoder, record.EventRecorder) capsulewebhook.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		return nil
	}
}
