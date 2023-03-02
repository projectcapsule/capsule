// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package pod

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulev1beta2 "github.com/clastix/capsule/api/v1beta2"
	capsulewebhook "github.com/clastix/capsule/pkg/webhook"
	"github.com/clastix/capsule/pkg/webhook/utils"
)

type containerRegistryHandler struct{}

func ContainerRegistry() capsulewebhook.Handler {
	return &containerRegistryHandler{}
}

func (h *containerRegistryHandler) OnCreate(c client.Client, decoder *admission.Decoder, recorder record.EventRecorder) capsulewebhook.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		return h.validate(ctx, c, decoder, recorder, req)
	}
}

func (h *containerRegistryHandler) OnDelete(client.Client, *admission.Decoder, record.EventRecorder) capsulewebhook.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		return nil
	}
}

// ust be validate on update events since updates to pods on spec.containers[*].image and spec.initContainers[*].image are allowed.
func (h *containerRegistryHandler) OnUpdate(c client.Client, decoder *admission.Decoder, recorder record.EventRecorder) capsulewebhook.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		return h.validate(ctx, c, decoder, recorder, req)
	}
}

func (h *containerRegistryHandler) validate(ctx context.Context, c client.Client, decoder *admission.Decoder, recorder record.EventRecorder, req admission.Request) *admission.Response {
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

	tnt := tntList.Items[0]

	if tnt.Spec.ContainerRegistries != nil {
		// Evaluate init containers
		for _, container := range pod.Spec.InitContainers {
			if response := h.VerifyContainerRegistry(recorder, req, container, tnt); response != nil {
				return response
			}
		}

		// Evaluate containers
		for _, container := range pod.Spec.Containers {
			if response := h.VerifyContainerRegistry(recorder, req, container, tnt); response != nil {
				return response
			}
		}
	}

	return nil
}

func (h *containerRegistryHandler) VerifyContainerRegistry(recorder record.EventRecorder, req admission.Request, container corev1.Container, tnt capsulev1beta2.Tenant) *admission.Response {
	var valid, matched bool

	reg := NewRegistry(container.Image)

	if len(reg.Registry()) == 0 {
		recorder.Eventf(&tnt, corev1.EventTypeWarning, "MissingFQCI", "Pod %s/%s is not using a fully qualified container image, cannot enforce registry the current Tenant", req.Namespace, req.Name, reg.Registry())

		response := admission.Denied(NewContainerRegistryForbidden(container.Image, *tnt.Spec.ContainerRegistries).Error())

		return &response
	}

	valid = tnt.Spec.ContainerRegistries.ExactMatch(reg.Registry())

	matched = tnt.Spec.ContainerRegistries.RegexMatch(reg.Registry())

	if !valid && !matched {
		recorder.Eventf(&tnt, corev1.EventTypeWarning, "ForbiddenContainerRegistry", "Pod %s/%s is using a container hosted on registry %s that is forbidden for the current Tenant", req.Namespace, req.Name, reg.Registry())

		response := admission.Denied(NewContainerRegistryForbidden(container.Image, *tnt.Spec.ContainerRegistries).Error())

		return &response
	}

	return nil
}
