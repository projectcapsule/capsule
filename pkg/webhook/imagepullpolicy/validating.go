// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package imagepullpolicy

import (
	"context"
	"net/http"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	"github.com/clastix/capsule/api/v1alpha1"
	"github.com/clastix/capsule/api/v1alpha1/domain"
	capsulewebhook "github.com/clastix/capsule/pkg/webhook"
)

// +kubebuilder:webhook:path=/validating-imagepullpolicy,mutating=false,sideEffects=None,admissionReviewVersions=v1,failurePolicy=fail,groups="",resources=pods,verbs=create,versions=v1,name=validating-image-pull-policy.capsule.clastix.io

type webhook struct {
	handler capsulewebhook.Handler
}

func Webhook(handler capsulewebhook.Handler) capsulewebhook.Webhook {
	return &webhook{handler: handler}
}

func (w *webhook) GetHandler() capsulewebhook.Handler {
	return w.handler
}

func (w *webhook) GetName() string {
	return "ImagePullPolicy"
}

func (w *webhook) GetPath() string {
	return "/validating-imagepullpolicy"
}

type handler struct{}

func Handler() capsulewebhook.Handler {
	return &handler{}
}

func (r *handler) OnCreate(c client.Client, decoder *admission.Decoder, recorder record.EventRecorder) capsulewebhook.Func {
	return func(ctx context.Context, req admission.Request) admission.Response {
		var pod = &corev1.Pod{}

		if err := decoder.Decode(req, pod); err != nil {
			return admission.Errored(http.StatusBadRequest, err)
		}

		var tntList = &v1alpha1.TenantList{}

		if err := c.List(ctx, tntList, client.MatchingFieldsSelector{
			Selector: fields.OneTermEqualSelector(".status.namespaces", pod.Namespace),
		}); err != nil {
			return admission.Errored(http.StatusBadRequest, err)
		}
		// the Pod is not running in a Namespace managed by a Tenant
		if len(tntList.Items) == 0 {
			return admission.Allowed("")
		}

		tnt := tntList.Items[0]

		policy := domain.NewImagePullPolicy(&tnt)
		// if Tenant doesn't enforce the pull policy, exit
		if policy == nil {
			return admission.Allowed("")
		}

		for _, container := range pod.Spec.Containers {
			usedPullPolicy := string(container.ImagePullPolicy)

			if !policy.IsPolicySupported(usedPullPolicy) {
				recorder.Eventf(&tnt, corev1.EventTypeWarning, "ForbiddenPullPolicy", "Pod %s/%s pull policy %s is forbidden for the current Tenant", req.Namespace, req.Name, usedPullPolicy)

				return admission.Denied(NewImagePullPolicyForbidden(usedPullPolicy, container.Name, policy.AllowedPullPolicies()).Error())
			}
		}

		return admission.Allowed("")
	}
}

func (r *handler) OnUpdate(client.Client, *admission.Decoder, record.EventRecorder) capsulewebhook.Func {
	return func(ctx context.Context, req admission.Request) admission.Response {
		return admission.Allowed("")
	}
}

func (r *handler) OnDelete(client.Client, *admission.Decoder, record.EventRecorder) capsulewebhook.Func {
	return func(ctx context.Context, req admission.Request) admission.Response {
		return admission.Allowed("")
	}
}
