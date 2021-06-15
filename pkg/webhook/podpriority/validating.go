// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package podpriority

import (
	"context"
	"net/http"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulev1alpha1 "github.com/clastix/capsule/api/v1alpha1"
	"github.com/clastix/capsule/api/v1alpha1/domain"
	capsulewebhook "github.com/clastix/capsule/pkg/webhook"
)

// +kubebuilder:webhook:path=/validating-v1-podpriority,mutating=false,sideEffects=None,admissionReviewVersions=v1,failurePolicy=ignore,groups="",resources=pods,verbs=create,versions=v1,name=podpriority.capsule.clastix.io

type webhook struct {
	handler capsulewebhook.Handler
}

func Webhook(handler capsulewebhook.Handler) capsulewebhook.Webhook {
	return &webhook{handler: handler}
}

func (w *webhook) GetName() string {
	return "podpriority"
}

func (w *webhook) GetPath() string {
	return "/validating-v1-podpriority"
}

func (w *webhook) GetHandler() capsulewebhook.Handler {
	return w.handler
}

type handler struct {
}

func Handler() capsulewebhook.Handler {
	return &handler{}
}

func (h *handler) OnCreate(c client.Client, decoder *admission.Decoder, recorder record.EventRecorder) capsulewebhook.Func {
	return func(ctx context.Context, req admission.Request) admission.Response {
		var pod = &corev1.Pod{}

		if err := decoder.Decode(req, pod); err != nil {
			return admission.Errored(http.StatusBadRequest, err)
		}

		var tntList = &capsulev1alpha1.TenantList{}

		if err := c.List(ctx, tntList, client.MatchingFieldsSelector{
			Selector: fields.OneTermEqualSelector(".status.namespaces", pod.Namespace),
		}); err != nil {
			return admission.Errored(http.StatusBadRequest, err)
		}

		if len(tntList.Items) == 0 {
			return admission.Allowed("")
		}

		var allowed = domain.NewPodPriority(&tntList.Items[0])

		var priorityClassName = pod.Spec.PriorityClassName

		switch {
		case allowed == nil:
			// Enforcement is not in place, skipping it at all
			return admission.Allowed("")
		case len(priorityClassName) == 0:
			// We don't have to force Pod to specify a Priority Class
			return admission.Allowed("")
		case !allowed.ExactMatch(priorityClassName) && !allowed.RegexMatch(priorityClassName):
			recorder.Eventf(&tntList.Items[0], corev1.EventTypeWarning, "ForbiddenPriorityClass", "Pod %s/%s is using Priority Class %s is forbidden for the current Tenant", pod.Namespace, pod.Name, priorityClassName)

			return admission.Errored(http.StatusBadRequest, NewPodPriorityClassForbidden(priorityClassName, *allowed))
		default:
			return admission.Allowed("")
		}
	}
}

func (h *handler) OnDelete(client.Client, *admission.Decoder, record.EventRecorder) capsulewebhook.Func {
	return func(ctx context.Context, req admission.Request) admission.Response {
		return admission.Allowed("")
	}
}

func (h *handler) OnUpdate(client.Client, *admission.Decoder, record.EventRecorder) capsulewebhook.Func {
	return func(ctx context.Context, req admission.Request) admission.Response {
		return admission.Allowed("")
	}
}
