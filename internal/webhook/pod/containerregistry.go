// Copyright 2020-2025 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package pod

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	capsulewebhook "github.com/projectcapsule/capsule/internal/webhook"
	"github.com/projectcapsule/capsule/internal/webhook/utils"
	"github.com/projectcapsule/capsule/pkg/configuration"
	"github.com/projectcapsule/capsule/pkg/utils/tenant"
)

type containerRegistryHandler struct {
	configuration configuration.Configuration
}

func ContainerRegistry(configuration configuration.Configuration) capsulewebhook.Handler {
	return &containerRegistryHandler{
		configuration: configuration,
	}
}

func (h *containerRegistryHandler) OnCreate(c client.Client, decoder admission.Decoder, recorder record.EventRecorder) capsulewebhook.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		return h.validate(ctx, c, decoder, recorder, req)
	}
}

func (h *containerRegistryHandler) OnDelete(client.Client, admission.Decoder, record.EventRecorder) capsulewebhook.Func {
	return func(context.Context, admission.Request) *admission.Response {
		return nil
	}
}

// ust be validate on update events since updates to pods on spec.containers[*].image and spec.initContainers[*].image are allowed.
func (h *containerRegistryHandler) OnUpdate(c client.Client, decoder admission.Decoder, recorder record.EventRecorder) capsulewebhook.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		return h.validate(ctx, c, decoder, recorder, req)
	}
}

func (h *containerRegistryHandler) validate(
	ctx context.Context,
	c client.Client,
	decoder admission.Decoder,
	recorder record.EventRecorder,
	req admission.Request,
) *admission.Response {
	pod := &corev1.Pod{}
	if err := decoder.Decode(req, pod); err != nil {
		return utils.ErroredResponse(err)
	}

	tnt, err := tenant.TenantByStatusNamespace(ctx, c, pod.GetNamespace())
	if err != nil {
		return utils.ErroredResponse(err)
	}

	if tnt == nil {
		return nil
	}

	if tnt.Spec.ContainerRegistries == nil {
		return nil
	}

	for _, container := range pod.Spec.InitContainers {
		if response := h.verifyContainerRegistry(recorder, req, container.Image, tnt); response != nil {
			return response
		}
	}

	for _, container := range pod.Spec.EphemeralContainers {
		if response := h.verifyContainerRegistry(recorder, req, container.Image, tnt); response != nil {
			return response
		}
	}

	for _, container := range pod.Spec.Containers {
		if response := h.verifyContainerRegistry(recorder, req, container.Image, tnt); response != nil {
			return response
		}
	}

	return nil
}

func (h *containerRegistryHandler) verifyContainerRegistry(
	recorder record.EventRecorder,
	req admission.Request,
	image string,
	tnt *capsulev1beta2.Tenant,
) *admission.Response {
	var valid, matched bool

	reg := NewRegistry(image, h.configuration)

	if len(reg.Registry()) == 0 {
		recorder.Eventf(tnt, corev1.EventTypeWarning, "MissingFQCI", "Pod %s/%s is not using a fully qualified container image, cannot enforce registry the current Tenant", req.Namespace, req.Name, reg.Registry())

		response := admission.Denied(NewContainerRegistryForbidden(image, *tnt.Spec.ContainerRegistries).Error())

		return &response
	}

	valid = tnt.Spec.ContainerRegistries.ExactMatch(reg.Registry())

	matched = tnt.Spec.ContainerRegistries.RegexMatch(reg.Registry())

	if !valid && !matched {
		recorder.Eventf(tnt, corev1.EventTypeWarning, "ForbiddenContainerRegistry", "Pod %s/%s is using a container hosted on registry %s that is forbidden for the current Tenant", req.Namespace, req.Name, reg.Registry())

		response := admission.Denied(NewContainerRegistryForbidden(reg.FQCI(), *tnt.Spec.ContainerRegistries).Error())

		return &response
	}

	return nil
}
