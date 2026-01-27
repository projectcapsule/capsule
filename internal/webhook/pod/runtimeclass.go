// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package pod

import (
	"context"
	"net/http"

	corev1 "k8s.io/api/core/v1"
	nodev1 "k8s.io/api/node/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/events"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	caperrors "github.com/projectcapsule/capsule/pkg/api/errors"
	evt "github.com/projectcapsule/capsule/pkg/runtime/events"
	"github.com/projectcapsule/capsule/pkg/runtime/handlers"
)

type runtimeClass struct{}

func RuntimeClass() handlers.TypedHandlerWithTenantWithRuleset[*corev1.Pod] {
	return &runtimeClass{}
}

func (h *runtimeClass) OnCreate(
	c client.Client,
	pod *corev1.Pod,
	decoder admission.Decoder,
	recorder events.EventRecorder,
	tnt *capsulev1beta2.Tenant,
	_ *capsulev1beta2.NamespaceRuleBody,
) handlers.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		return h.validate(ctx, c, recorder, req, pod, tnt)
	}
}

func (h *runtimeClass) OnUpdate(
	client.Client,
	*corev1.Pod,
	*corev1.Pod,
	admission.Decoder,
	events.EventRecorder,
	*capsulev1beta2.Tenant,
	*capsulev1beta2.NamespaceRuleBody,
) handlers.Func {
	return func(context.Context, admission.Request) *admission.Response {
		return nil
	}
}

func (h *runtimeClass) OnDelete(
	client.Client,
	*corev1.Pod,
	admission.Decoder,
	events.EventRecorder,
	*capsulev1beta2.Tenant,
	*capsulev1beta2.NamespaceRuleBody,
) handlers.Func {
	return func(context.Context, admission.Request) *admission.Response {
		return nil
	}
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

func (h *runtimeClass) validate(
	ctx context.Context,
	c client.Client,
	recorder events.EventRecorder,
	req admission.Request,
	pod *corev1.Pod,
	tnt *capsulev1beta2.Tenant,
) *admission.Response {
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
	case len(runtimeClassName) == 0 || runtimeClassName == allowed.Default:
		// Delegating mutating webhook to specify a default RuntimeClass
		return nil
	case !allowed.MatchSelectByName(class):
		recorder.Eventf(
			tnt,
			pod,
			corev1.EventTypeWarning,
			evt.ReasonForbiddenRuntimeClass,
			evt.ActionValidationDenied,
			"Using Runtime Class %s is forbidden for the tenant %s", runtimeClassName, tnt.GetName(),
		)

		response := admission.Denied(caperrors.NewPodRuntimeClassForbidden(runtimeClassName, *allowed).Error())

		return &response
	default:
		return nil
	}
}
