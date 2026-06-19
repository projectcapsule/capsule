// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package pod

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	caperrors "github.com/projectcapsule/capsule/pkg/api/errors"
	"github.com/projectcapsule/capsule/pkg/api/rules"
	ad "github.com/projectcapsule/capsule/pkg/runtime/admission"
	"github.com/projectcapsule/capsule/pkg/runtime/configuration"
	"github.com/projectcapsule/capsule/pkg/runtime/events"
	"github.com/projectcapsule/capsule/pkg/runtime/handlers"
)

type containerRegistryLegacyHandler struct {
	configuration configuration.Configuration
}

func ContainerRegistryLegacy(configuration configuration.Configuration) handlers.TypedHandlerWithTenantWithRuleset[*corev1.Pod] {
	return &containerRegistryLegacyHandler{
		configuration: configuration,
	}
}

func (h *containerRegistryLegacyHandler) OnCreate(
	_ client.Client,
	_ client.Reader,
	pod *corev1.Pod,
	_ admission.Decoder,
	recorder events.EventRecorder,
	tnt *capsulev1beta2.Tenant,
	_ []*rules.NamespaceRuleBodyNamespace,
) handlers.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		return h.validate(ctx, req, pod, tnt, recorder)
	}
}

func (h *containerRegistryLegacyHandler) OnUpdate(
	_ client.Client,
	_ client.Reader,
	old *corev1.Pod,
	pod *corev1.Pod,
	_ admission.Decoder,
	recorder events.EventRecorder,
	tnt *capsulev1beta2.Tenant,
	_ []*rules.NamespaceRuleBodyNamespace,
) handlers.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		return h.validate(ctx, req, pod, tnt, recorder)
	}
}

func (h *containerRegistryLegacyHandler) OnDelete(
	client.Client,
	client.Reader,
	*corev1.Pod,
	admission.Decoder,
	events.EventRecorder,
	*capsulev1beta2.Tenant,
	[]*rules.NamespaceRuleBodyNamespace,
) handlers.Func {
	return func(context.Context, admission.Request) *admission.Response {
		return nil
	}
}

func (h *containerRegistryLegacyHandler) validate(
	ctx context.Context,
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
		if response := h.verifyContainerRegistry(ctx, recorder, pod, req, container.Image, tnt); response != nil {
			return response
		}
	}

	for _, container := range pod.Spec.EphemeralContainers {
		if response := h.verifyContainerRegistry(ctx, recorder, pod, req, container.Image, tnt); response != nil {
			return response
		}
	}

	for _, container := range pod.Spec.Containers {
		if response := h.verifyContainerRegistry(ctx, recorder, pod, req, container.Image, tnt); response != nil {
			return response
		}
	}

	return nil
}

func (h *containerRegistryLegacyHandler) verifyContainerRegistry(
	ctx context.Context,
	recorder events.EventRecorder,
	pod *corev1.Pod,
	req admission.Request,
	image string,
	tnt *capsulev1beta2.Tenant,
) *admission.Response {
	var valid, matched bool

	reg := NewRegistry(image, h.configuration)

	if len(reg.Registry()) == 0 {
		recorder.LabeledEvent(
			pod,
			corev1.EventTypeWarning,
			events.ReasonMissingFQCI,
			events.ActionValidationDenied,
			fmt.Sprintf("container image %q is not fully qualified (missing registry), cannot enforce tenant registry rules", image),
		).
			WithRelated(tnt).
			WithTenantLabel(tnt).
			WithRequestAnnotations(req).
			Emit(ctx)

		//nolint:staticcheck
		return ad.Deny(caperrors.NewContainerRegistryForbidden(image, *tnt.Spec.ContainerRegistries).Error())
	}

	//nolint:staticcheck
	valid = tnt.Spec.ContainerRegistries.ExactMatch(reg.Registry())

	//nolint:staticcheck
	matched = tnt.Spec.ContainerRegistries.RegexMatch(reg.Registry())

	if !valid && !matched {
		recorder.LabeledEvent(
			pod,
			corev1.EventTypeWarning,
			events.ReasonForbiddenContainerRegistry,
			events.ActionValidationDenied,
			fmt.Sprintf("using a container hosted on registry %s that is forbidden for the tenant", reg.Registry()),
		).
			WithRelated(tnt).
			WithTenantLabel(tnt).
			WithRequestAnnotations(req).
			Emit(ctx)

		//nolint:staticcheck
		return ad.Deny(caperrors.NewContainerRegistryForbidden(reg.FQCI(), *tnt.Spec.ContainerRegistries).Error())
	}

	return nil
}
