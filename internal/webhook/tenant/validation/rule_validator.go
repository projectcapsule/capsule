// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package validation

import (
	"context"
	"fmt"
	"regexp"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/events"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/runtime/handlers"
)

type RuleValidationHandler struct{}

func RuleHandler() handlers.TypedHandler[*capsulev1beta2.Tenant] {
	return &RuleValidationHandler{}
}

func (h *RuleValidationHandler) OnCreate(
	_ client.Client,
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

func (h *RuleValidationHandler) OnDelete(client.Client, *capsulev1beta2.Tenant, admission.Decoder, events.EventRecorder) handlers.Func {
	return func(context.Context, admission.Request) *admission.Response {
		return nil
	}
}

func (h *RuleValidationHandler) OnUpdate(
	_ client.Client,
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
	if len(tnt.Spec.Rules) == 0 {
		return nil
	}

	// Validate Rules
	for i, rule := range tnt.Spec.Rules {
		if rule == nil {
			continue
		}

		// Validate NamespaceSelector (if provided)
		if rule.NamespaceSelector != nil {
			if _, err := metav1.LabelSelectorAsSelector(rule.NamespaceSelector); err != nil {
				resp := admission.Denied(
					fmt.Sprintf("rules[%d].namespaceSelector is invalid: %v", i, err),
				)

				return &resp
			}
		}

		// Validate Registries
		for _, r := range rule.Enforce.Registries {
			if _, err := regexp.Compile(r.Registry); err != nil {
				resp := admission.Denied(
					fmt.Sprintf("unable to compile regex %q: %v", r.Registry, err),
				)

				return &resp
			}
		}
	}

	return nil
}
