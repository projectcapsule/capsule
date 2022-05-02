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

	capsulev1beta1 "github.com/clastix/capsule/api/v1beta1"
	capsulewebhook "github.com/clastix/capsule/pkg/webhook"
	"github.com/clastix/capsule/pkg/webhook/utils"
)

type imagePullPolicy struct{}

func ImagePullPolicy() capsulewebhook.Handler {
	return &imagePullPolicy{}
}

func (r *imagePullPolicy) OnCreate(c client.Client, decoder *admission.Decoder, recorder record.EventRecorder) capsulewebhook.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		pod := &corev1.Pod{}
		if err := decoder.Decode(req, pod); err != nil {
			return utils.ErroredResponse(err)
		}

		tntList := &capsulev1beta1.TenantList{}
		if err := c.List(ctx, tntList, client.MatchingFieldsSelector{
			Selector: fields.OneTermEqualSelector(".status.namespaces", pod.Namespace),
		}); err != nil {
			return utils.ErroredResponse(err)
		}
		// the Pod is not running in a Namespace managed by a Tenant
		if len(tntList.Items) == 0 {
			return nil
		}

		tnt := tntList.Items[0]

		policy := NewPullPolicy(&tnt)
		// if Tenant doesn't enforce the pull policy, exit
		if policy == nil {
			return nil
		}

		for _, container := range pod.Spec.Containers {
			usedPullPolicy := string(container.ImagePullPolicy)

			if !policy.IsPolicySupported(usedPullPolicy) {
				recorder.Eventf(&tnt, corev1.EventTypeWarning, "ForbiddenPullPolicy", "Pod %s/%s pull policy %s is forbidden for the current Tenant", req.Namespace, req.Name, usedPullPolicy)

				response := admission.Denied(NewImagePullPolicyForbidden(usedPullPolicy, container.Name, policy.AllowedPullPolicies()).Error())

				return &response
			}
		}

		return nil
	}
}

func (r *imagePullPolicy) OnUpdate(client.Client, *admission.Decoder, record.EventRecorder) capsulewebhook.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		return nil
	}
}

func (r *imagePullPolicy) OnDelete(client.Client, *admission.Decoder, record.EventRecorder) capsulewebhook.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		return nil
	}
}
