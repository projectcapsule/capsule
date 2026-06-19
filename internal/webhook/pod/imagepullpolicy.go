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
	"github.com/projectcapsule/capsule/pkg/runtime/events"
	"github.com/projectcapsule/capsule/pkg/runtime/handlers"
)

type imagePullPolicy struct{}

func ImagePullPolicy() handlers.TypedHandlerWithTenantWithRuleset[*corev1.Pod] {
	return &imagePullPolicy{}
}

func (h *imagePullPolicy) OnCreate(
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

func (h *imagePullPolicy) OnUpdate(
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

func (h *imagePullPolicy) OnDelete(
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

func (h *imagePullPolicy) validate(
	ctx context.Context,
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
		if response := h.verifyPullPolicy(ctx, recorder, pod, req, policy, string(container.ImagePullPolicy), container.Name, tnt); response != nil {
			return response
		}
	}

	for _, container := range pod.Spec.EphemeralContainers {
		if response := h.verifyPullPolicy(ctx, recorder, pod, req, policy, string(container.ImagePullPolicy), container.Name, tnt); response != nil {
			return response
		}
	}

	for _, container := range pod.Spec.Containers {
		if response := h.verifyPullPolicy(ctx, recorder, pod, req, policy, string(container.ImagePullPolicy), container.Name, tnt); response != nil {
			return response
		}
	}

	return nil
}

func (h *imagePullPolicy) verifyPullPolicy(
	ctx context.Context,
	recorder events.EventRecorder,
	pod *corev1.Pod,
	req admission.Request,
	policy PullPolicy,
	usedPullPolicy string,
	container string,
	tnt *capsulev1beta2.Tenant,
) *admission.Response {
	if !policy.IsPolicySupported(usedPullPolicy) {
		recorder.LabeledEvent(
			pod,
			corev1.EventTypeWarning,
			events.ReasonForbiddenPullPolicy,
			events.ActionValidationDenied,
			fmt.Sprintf("using pullpolicy %s is forbidden for the tenant", usedPullPolicy),
		).
			WithRelated(tnt).
			WithTenantLabel(tnt).
			WithRequestAnnotations(req).
			Emit(ctx)

		return ad.Deny(caperrors.NewImagePullPolicyForbidden(usedPullPolicy, container, policy.AllowedPullPolicies()).Error())
	}

	return nil
}
