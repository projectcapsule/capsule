// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package pod

import (
	"context"
	"net/http"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/tools/events"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/internal/webhook/utils"
	evt "github.com/projectcapsule/capsule/pkg/runtime/events"
	"github.com/projectcapsule/capsule/pkg/runtime/handlers"
)

type priorityClass struct{}

func PriorityClass() handlers.TypedHandlerWithTenantWithRuleset[*corev1.Pod] {
	return &priorityClass{}
}

func (h *priorityClass) OnCreate(
	c client.Client,
	pod *corev1.Pod,
	decoder admission.Decoder,
	recorder events.EventRecorder,
	tnt *capsulev1beta2.Tenant,
	_ *capsulev1beta2.NamespaceRuleBody,
) handlers.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
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
			recorder.Eventf(
				pod,
				tnt,
				corev1.EventTypeWarning,
				evt.ReasonForbiddenPriorityClass,
				evt.ActionValidationDenied,
				"Using Priority Class %s is forbidden for the tenant %s", priorityClassName, tnt.GetName(),
			)

			response := admission.Denied(NewPodPriorityClassForbidden(priorityClassName, *allowed).Error())

			return &response
		}
	}
}

func (h *priorityClass) OnUpdate(
	client.Client,
	*corev1.Pod,
	*corev1.Pod,
	admission.Decoder,
	events.EventRecorder,
	*capsulev1beta2.Tenant,
	*capsulev1beta2.NamespaceRuleBody,
) handlers.Func {
	return func(context.Context, admission.Request) *admission.Response {
		return nil
	}
}

func (h *priorityClass) OnDelete(
	client.Client,
	*corev1.Pod,
	admission.Decoder,
	events.EventRecorder,
	*capsulev1beta2.Tenant,
	*capsulev1beta2.NamespaceRuleBody,
) handlers.Func {
	return func(context.Context, admission.Request) *admission.Response {
		return nil
	}
}
