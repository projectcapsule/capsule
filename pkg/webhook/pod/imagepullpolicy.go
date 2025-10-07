// Copyright 2020-2025 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package pod

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	capsulewebhook "github.com/projectcapsule/capsule/pkg/webhook"
	"github.com/projectcapsule/capsule/pkg/webhook/utils"
)

type imagePullPolicy struct{}

func ImagePullPolicy() capsulewebhook.Handler {
	return &imagePullPolicy{}
}

func (r *imagePullPolicy) OnCreate(c client.Client, decoder admission.Decoder, recorder record.EventRecorder) capsulewebhook.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		return r.validate(ctx, c, decoder, recorder, req)
	}
}

func (r *imagePullPolicy) OnUpdate(c client.Client, decoder admission.Decoder, recorder record.EventRecorder) capsulewebhook.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		return r.validate(ctx, c, decoder, recorder, req)
	}
}

func (r *imagePullPolicy) OnDelete(client.Client, admission.Decoder, record.EventRecorder) capsulewebhook.Func {
	return func(context.Context, admission.Request) *admission.Response {
		return nil
	}
}

func (h *imagePullPolicy) validate(
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

	policy := NewPullPolicy(&tnt)
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
	tnt capsulev1beta2.Tenant,
) *admission.Response {
	if !policy.IsPolicySupported(usedPullPolicy) {
		recorder.Eventf(&tnt, corev1.EventTypeWarning, "ForbiddenPullPolicy", "Pod %s/%s pull policy %s is forbidden for the current Tenant", req.Namespace, req.Name, usedPullPolicy)

		response := admission.Denied(NewImagePullPolicyForbidden(usedPullPolicy, container, policy.AllowedPullPolicies()).Error())

		return &response
	}

	return nil
}
