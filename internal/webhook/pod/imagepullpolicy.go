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
	evt "github.com/projectcapsule/capsule/pkg/runtime/events"
)

type imagePullPolicy struct{}

func ImagePullPolicy() capsulewebhook.TypedHandlerWithTenant[*corev1.Pod] {
	return &imagePullPolicy{}
}

func (h *imagePullPolicy) OnCreate(
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

func (h *imagePullPolicy) OnUpdate(
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

func (h *imagePullPolicy) OnDelete(
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

func (h *imagePullPolicy) validate(
	req admission.Request,
	pod *corev1.Pod,
	tnt *capsulev1beta2.Tenant,
	recorder events.EventRecorder,
) *admission.Response {
	policy := NewPullPolicy(tnt)
	if policy == nil {
		return nil
	}

	for _, container := range pod.Spec.InitContainers {
		if response := h.verifyPullPolicy(recorder, pod, req, policy, string(container.ImagePullPolicy), container.Name, tnt); response != nil {
			return response
		}
	}

	for _, container := range pod.Spec.EphemeralContainers {
		if response := h.verifyPullPolicy(recorder, pod, req, policy, string(container.ImagePullPolicy), container.Name, tnt); response != nil {
			return response
		}
	}

	for _, container := range pod.Spec.Containers {
		if response := h.verifyPullPolicy(recorder, pod, req, policy, string(container.ImagePullPolicy), container.Name, tnt); response != nil {
			return response
		}
	}

	return nil
}

func (h *imagePullPolicy) verifyPullPolicy(
	recorder events.EventRecorder,
	pod *corev1.Pod,
	req admission.Request,
	policy PullPolicy,
	usedPullPolicy string,
	container string,
	tnt *capsulev1beta2.Tenant,
) *admission.Response {
	if !policy.IsPolicySupported(usedPullPolicy) {
		recorder.Eventf(
			pod,
			tnt,
			corev1.EventTypeWarning,
			evt.ReasonForbiddenPullPolicy,
			evt.ActionValidationDenied,
			"PullPolicy %s is forbidden for the tenant %s", usedPullPolicy, tnt.GetName(),
		)

		response := admission.Denied(NewImagePullPolicyForbidden(usedPullPolicy, container, policy.AllowedPullPolicies()).Error())

		return &response
	}

	return nil
}
