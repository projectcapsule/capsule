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
)

type imagePullPolicy struct{}

func ImagePullPolicy() capsulewebhook.TypedHandlerWithTenant[*corev1.Pod] {
	return &imagePullPolicy{}
}

func (h *imagePullPolicy) OnCreate(
	c client.Client,
	pod *corev1.Pod,
	decoder admission.Decoder,
	recorder record.EventRecorder,
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
	recorder record.EventRecorder,
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
	record.EventRecorder,
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
	recorder record.EventRecorder,
) *admission.Response {
	policy := NewPullPolicy(tnt)
	if policy == nil {
		return nil
	}

	for _, container := range pod.Spec.InitContainers {
		if response := h.verifyPullPolicy(recorder, req, policy, string(container.ImagePullPolicy), container.Name, tnt); response != nil {
			return response
		}
	}

	for _, container := range pod.Spec.EphemeralContainers {
		if response := h.verifyPullPolicy(recorder, req, policy, string(container.ImagePullPolicy), container.Name, tnt); response != nil {
			return response
		}
	}

	for _, container := range pod.Spec.Containers {
		if response := h.verifyPullPolicy(recorder, req, policy, string(container.ImagePullPolicy), container.Name, tnt); response != nil {
			return response
		}
	}

	return nil
}

func (h *imagePullPolicy) verifyPullPolicy(
	recorder record.EventRecorder,
	req admission.Request,
	policy PullPolicy,
	usedPullPolicy string,
	container string,
	tnt *capsulev1beta2.Tenant,
) *admission.Response {
	if !policy.IsPolicySupported(usedPullPolicy) {
		recorder.Eventf(tnt, corev1.EventTypeWarning, "ForbiddenPullPolicy", "Pod %s/%s pull policy %s is forbidden for the current Tenant", req.Namespace, req.Name, usedPullPolicy)

		response := admission.Denied(NewImagePullPolicyForbidden(usedPullPolicy, container, policy.AllowedPullPolicies()).Error())

		return &response
	}

	return nil
}
