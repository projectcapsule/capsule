// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package pod

import (
	"context"
	"net/http"

	corev1 "k8s.io/api/core/v1"
	schedulingv1 "k8s.io/api/scheduling/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulev1beta2 "github.com/clastix/capsule/api/v1beta2"
	capsulewebhook "github.com/clastix/capsule/pkg/webhook"
	"github.com/clastix/capsule/pkg/webhook/utils"
)

type priorityClass struct{}

func PriorityClass() capsulewebhook.Handler {
	return &priorityClass{}
}

func (h *priorityClass) class(ctx context.Context, c client.Client, name string) (client.Object, error) {
	if len(name) == 0 {
		return nil, nil
	}

	obj := &schedulingv1.PriorityClass{}

	if err := c.Get(ctx, types.NamespacedName{Name: name}, obj); err != nil {
		if errors.IsNotFound(err) {
			return nil, nil
		}

		return nil, err
	}

	return obj, nil
}

func (h *priorityClass) OnCreate(c client.Client, decoder *admission.Decoder, recorder record.EventRecorder) capsulewebhook.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		pod := &corev1.Pod{}
		if err := decoder.Decode(req, pod); err != nil {
			return utils.ErroredResponse(err)
		}

		tntList := &capsulev1beta2.TenantList{}

		if err := c.List(ctx, tntList, client.MatchingFieldsSelector{
			Selector: fields.OneTermEqualSelector(".status.namespaces", pod.Namespace),
		}); err != nil {
			return utils.ErroredResponse(err)
		}

		if len(tntList.Items) == 0 {
			return nil
		}

		allowed := tntList.Items[0].Spec.PriorityClasses

		priorityClassName := pod.Spec.PriorityClassName

		class, err := h.class(ctx, c, priorityClassName)
		if err != nil {
			response := admission.Errored(http.StatusInternalServerError, err)

			return &response
		}

		switch {
		case allowed == nil:
			// Enforcement is not in place, skipping it at all
			return nil
		case len(priorityClassName) == 0:
			// We don't have to force Pod to specify a Priority Class
			return nil
		case !allowed.ExactMatch(priorityClassName) && !allowed.RegexMatch(priorityClassName) && !allowed.SelectorMatch(class):
			recorder.Eventf(&tntList.Items[0], corev1.EventTypeWarning, "ForbiddenPriorityClass", "Pod %s/%s is using Priority Class %s is forbidden for the current Tenant", pod.Namespace, pod.Name, priorityClassName)

			response := admission.Denied(NewPodPriorityClassForbidden(priorityClassName, *allowed).Error())

			return &response
		default:
			return nil
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
