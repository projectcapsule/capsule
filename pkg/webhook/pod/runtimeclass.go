// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package pod

import (
	"context"
	"net/http"

	corev1 "k8s.io/api/core/v1"
	nodev1 "k8s.io/api/node/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulewebhook "github.com/clastix/capsule/pkg/webhook"
	"github.com/clastix/capsule/pkg/webhook/utils"
)

type runtimeClass struct{}

func RuntimeClass() capsulewebhook.Handler {
	return &runtimeClass{}
}

func (h *runtimeClass) class(ctx context.Context, c client.Client, name string) (client.Object, error) {
	if len(name) == 0 {
		return nil, nil
	}

	obj := &nodev1.RuntimeClass{}
	if err := c.Get(ctx, types.NamespacedName{Name: name}, obj); err != nil {
		return nil, err
	}

	return obj, nil
}

func (h *runtimeClass) OnCreate(c client.Client, decoder *admission.Decoder, recorder record.EventRecorder) capsulewebhook.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		return h.validate(ctx, c, decoder, recorder, req)
	}
}

func (h *runtimeClass) OnDelete(client.Client, *admission.Decoder, record.EventRecorder) capsulewebhook.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		return nil
	}
}

func (h *runtimeClass) OnUpdate(client.Client, *admission.Decoder, record.EventRecorder) capsulewebhook.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		return nil
	}
}

func (h *runtimeClass) validate(ctx context.Context, c client.Client, decoder *admission.Decoder, recorder record.EventRecorder, req admission.Request) *admission.Response {
	pod := &corev1.Pod{}
	if err := decoder.Decode(req, pod); err != nil {
		return utils.ErroredResponse(err)
	}

	tnt, err := utils.TenantByStatusNamespace(ctx, c, pod.Namespace)
	if err != nil {
		return utils.ErroredResponse(err)
	}

	if tnt == nil {
		return nil
	}

	allowed := tnt.Spec.RuntimeClasses

	runtimeClassName := ""
	if pod.Spec.RuntimeClassName != nil {
		runtimeClassName = *pod.Spec.RuntimeClassName
	}

	class, err := h.class(ctx, c, runtimeClassName)
	if err != nil {
		response := admission.Errored(http.StatusInternalServerError, err)

		return &response
	}

	switch {
	case allowed == nil:
		// Enforcement is not in place, skipping it at all
		return nil
	case len(runtimeClassName) == 0:
		// We don't have to force Pod to specify a RuntimeClass
		return nil
	case !allowed.MatchSelectByName(class):
		recorder.Eventf(tnt, corev1.EventTypeWarning, "ForbiddenRuntimeClass", "Pod %s/%s is using Runtime Class %s is forbidden for the current Tenant", pod.Namespace, pod.Name, runtimeClassName)

		response := admission.Denied(NewPodRuntimeClassForbidden(runtimeClassName, *allowed).Error())

		return &response
	default:
		return nil
	}
}
