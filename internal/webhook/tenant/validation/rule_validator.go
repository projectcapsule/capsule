// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package validation

import (
	"context"
	"regexp"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	ad "github.com/projectcapsule/capsule/pkg/runtime/admission"
	"github.com/projectcapsule/capsule/pkg/runtime/events"
	"github.com/projectcapsule/capsule/pkg/runtime/handlers"
)

type RuleValidationHandler struct{}

func RuleHandler() handlers.TypedHandler[*capsulev1beta2.Tenant] {
	return &RuleValidationHandler{}
}

func (h *RuleValidationHandler) OnCreate(
	_ client.Client,
	_ client.Reader,
	tnt *capsulev1beta2.Tenant,
	decoder admission.Decoder,
	_ events.EventRecorder,
) handlers.Func {
	return func(_ context.Context, req admission.Request) *admission.Response {
		if err := ValidateRule(tnt, req); err != nil {
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
		if response := ValidateRule(tnt, req); response != nil {
			return response
		}

		return nil
	}
}

func ValidateRule(tnt *capsulev1beta2.Tenant, req admission.Request) *admission.Response {
	if tnt == nil {
		return nil
	}

	if len(tnt.Spec.Rules) == 0 {
		return nil
	}

	for i, rule := range tnt.Spec.Rules {
		if rule == nil {
			continue
		}

		body := rule.NamespaceRuleBodyNamespace
		if body == nil {
			continue
		}

		if rule.Enforce == nil {
			continue
		}

		if rule.NamespaceSelector != nil {
			if _, err := metav1.LabelSelectorAsSelector(rule.NamespaceSelector); err != nil {
				return ad.Denyf("rules[%d].namespaceSelector is invalid: %v", i, err)
			}
		}

		for j, registry := range rule.Enforce.Workloads.Registries {
			expr := registry.Expression()

			if strings.TrimSpace(expr.Expression) == "" {
				return ad.Denyf("rules[%d].enforce.workloads.registries[%d].exp must not be empty", i, j)
			}

			if _, err := regexp.Compile(expr.Expression); err != nil {
				return ad.Denyf(
					"rules[%d].enforce.workloads.registries[%d].exp %q is invalid: %v",
					i,
					j,
					expr.Expression,
					err,
				)
			}
		}
	}

	return nil
}
