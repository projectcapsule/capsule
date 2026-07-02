// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package validation

import (
	"context"

	k8smeta "k8s.io/apimachinery/pkg/api/meta"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/rules"
	"github.com/projectcapsule/capsule/pkg/ruleengine"
	ad "github.com/projectcapsule/capsule/pkg/runtime/admission"
	"github.com/projectcapsule/capsule/pkg/runtime/events"
	"github.com/projectcapsule/capsule/pkg/runtime/handlers"
)

type RuleValidationHandler struct {
	mapper k8smeta.RESTMapper
}

func RuleHandler(mapper k8smeta.RESTMapper) handlers.TypedHandler[*capsulev1beta2.Tenant] {
	return &RuleValidationHandler{
		mapper: mapper,
	}
}

func (h *RuleValidationHandler) OnCreate(
	_ client.Client,
	_ client.Reader,
	tnt *capsulev1beta2.Tenant,
	decoder admission.Decoder,
	_ events.EventRecorder,
) handlers.Func {
	return func(_ context.Context, req admission.Request) *admission.Response {
		if err := h.handle(tnt, req); err != nil {
			return err
		}

		return nil
	}
}

func (h *RuleValidationHandler) OnDelete(
	client.Client,
	client.Reader,
	*capsulev1beta2.Tenant,
	admission.Decoder,
	events.EventRecorder,
) handlers.Func {
	return func(context.Context, admission.Request) *admission.Response {
		return nil
	}
}

func (h *RuleValidationHandler) OnUpdate(
	_ client.Client,
	_ client.Reader,
	tnt *capsulev1beta2.Tenant,
	old *capsulev1beta2.Tenant,
	decoder admission.Decoder,
	_ events.EventRecorder,
) handlers.Func {
	return func(_ context.Context, req admission.Request) *admission.Response {
		if response := h.handle(tnt, req); response != nil {
			return response
		}

		return nil
	}
}

func (h *RuleValidationHandler) handle(
	tnt *capsulev1beta2.Tenant,
	req admission.Request,
) *admission.Response {
	if tnt == nil || len(tnt.Spec.Rules) == 0 {
		return nil
	}

	bodies := make([]*rules.NamespaceRuleBodyNamespace, 0, len(tnt.Spec.Rules))

	for _, rule := range tnt.Spec.Rules {
		if rule == nil || rule.NamespaceRuleBodyNamespace == nil {
			continue
		}

		body := rule.NamespaceRuleBodyNamespace
		if body.Enforce == nil {
			continue
		}

		bodies = append(bodies, body)
	}

	if len(bodies) == 0 {
		return nil
	}

	if err := ruleengine.ValidateRuleStatusBody(h.mapper, bodies); err != nil {
		return ad.Deny(err.Error())
	}

	return nil
}
