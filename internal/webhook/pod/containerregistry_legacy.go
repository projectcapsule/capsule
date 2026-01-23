// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package pod

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/tools/events"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	capsulewebhook "github.com/projectcapsule/capsule/internal/webhook"
	"github.com/projectcapsule/capsule/pkg/configuration"
	evt "github.com/projectcapsule/capsule/pkg/runtime/events"
)

type containerRegistryLegacyHandler struct {
	configuration configuration.Configuration
}

func ContainerRegistryLegacy(configuration configuration.Configuration) capsulewebhook.TypedHandlerWithTenant[*corev1.Pod] {
	return &containerRegistryLegacyHandler{
		configuration: configuration,
	}
}

func (h *containerRegistryLegacyHandler) OnCreate(
	c client.Client,
	pod *corev1.Pod,
	decoder admission.Decoder,
	recorder events.EventRecorder,
	tnt *capsulev1beta2.Tenant,
) capsulewebhook.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		return h.validate(req, pod, tnt, recorder)
	}
}

func (h *containerRegistryLegacyHandler) OnUpdate(
	c client.Client,
	old *corev1.Pod,
	pod *corev1.Pod,
	decoder admission.Decoder,
	recorder events.EventRecorder,
	tnt *capsulev1beta2.Tenant,
) capsulewebhook.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		return h.validate(req, pod, tnt, recorder)
	}
}

func (h *containerRegistryLegacyHandler) OnDelete(
	client.Client,
	*corev1.Pod,
	admission.Decoder,
	events.EventRecorder,
	*capsulev1beta2.Tenant,
) capsulewebhook.Func {
	return func(context.Context, admission.Request) *admission.Response {
		return nil
	}
}

func (h *containerRegistryLegacyHandler) validate(
	req admission.Request,
	pod *corev1.Pod,
	tnt *capsulev1beta2.Tenant,
	recorder events.EventRecorder,
) *admission.Response {
	//nolint:staticcheck
	if tnt.Spec.ContainerRegistries == nil {
		return nil
	}

	for _, container := range pod.Spec.InitContainers {
		if response := h.verifyContainerRegistry(recorder, pod, req, container.Image, tnt); response != nil {
			return response
		}
	}

	for _, container := range pod.Spec.EphemeralContainers {
		if response := h.verifyContainerRegistry(recorder, pod, req, container.Image, tnt); response != nil {
			return response
		}
	}

	for _, container := range pod.Spec.Containers {
		if response := h.verifyContainerRegistry(recorder, pod, req, container.Image, tnt); response != nil {
			return response
		}
	}

	return nil
}

func (h *containerRegistryLegacyHandler) verifyContainerRegistry(
	recorder events.EventRecorder,
	pod *corev1.Pod,
	req admission.Request,
	image string,
	tnt *capsulev1beta2.Tenant,
) *admission.Response {
	var valid, matched bool

	reg := NewRegistry(image, h.configuration)

	if len(reg.Registry()) == 0 {
		recorder.Eventf(tnt, pod, corev1.EventTypeWarning, evt.ReasonMissingFQCI, evt.ActionValidationDenied, "Pod %s/%s is not using a fully qualified container image, cannot enforce registry the current Tenant", req.Namespace, req.Name, reg.Registry())

		//nolint:staticcheck
		response := admission.Denied(NewContainerRegistryForbidden(image, *tnt.Spec.ContainerRegistries).Error())

		return &response
	}

	//nolint:staticcheck
	valid = tnt.Spec.ContainerRegistries.ExactMatch(reg.Registry())

	//nolint:staticcheck
	matched = tnt.Spec.ContainerRegistries.RegexMatch(reg.Registry())

	if !valid && !matched {
		recorder.Eventf(tnt, pod, corev1.EventTypeWarning, evt.ReasonForbiddenContainerRegistry, evt.ActionValidationDenied, "Pod %s/%s is using a container hosted on registry %s that is forbidden for the current Tenant", req.Namespace, req.Name, reg.Registry())

		//nolint:staticcheck
		response := admission.Denied(NewContainerRegistryForbidden(reg.FQCI(), *tnt.Spec.ContainerRegistries).Error())

		return &response
	}

	return nil
}
