// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package cfg

import (
	"context"
	"fmt"
	"strings"

	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/internal/cache"
	"github.com/projectcapsule/capsule/pkg/api/runtime"
	ad "github.com/projectcapsule/capsule/pkg/runtime/admission"
	"github.com/projectcapsule/capsule/pkg/runtime/events"
	"github.com/projectcapsule/capsule/pkg/runtime/handlers"
)

type validationHandler struct {
	regexCache *cache.RegexCache
}

func ValidationHandler(regexCache *cache.RegexCache) handlers.TypedHandler[*capsulev1beta2.CapsuleConfiguration] {
	return &validationHandler{
		regexCache: regexCache,
	}
}

func (h *validationHandler) OnCreate(
	_ client.Client,
	_ client.Reader,
	cfg *capsulev1beta2.CapsuleConfiguration,
	_ admission.Decoder,
	_ events.EventRecorder,
) handlers.Func {
	return func(_ context.Context, req admission.Request) *admission.Response {
		return h.handle(cfg, req)
	}
}

func (h *validationHandler) OnDelete(
	client.Client,
	client.Reader,
	*capsulev1beta2.CapsuleConfiguration,
	admission.Decoder,
	events.EventRecorder,
) handlers.Func {
	return func(context.Context, admission.Request) *admission.Response {
		return nil
	}
}

func (h *validationHandler) OnUpdate(
	_ client.Client,
	_ client.Reader,
	cfg *capsulev1beta2.CapsuleConfiguration,
	old *capsulev1beta2.CapsuleConfiguration,
	_ admission.Decoder,
	_ events.EventRecorder,
) handlers.Func {
	return func(_ context.Context, req admission.Request) *admission.Response {
		return h.handle(cfg, req)
	}
}

func (h *validationHandler) handle(
	config *capsulev1beta2.CapsuleConfiguration,
	req admission.Request,
) *admission.Response {
	if err := h.validateRegex(
		"spec.protectedNamespaceRegex",
		config.Spec.ProtectedNamespaceRegexpString,
	); err != nil {
		return ad.Deny(err.Error())
	}

	if err := h.validateRegex(
		"spec.nodeMetadata.forbiddenAnnotations.regex",
		config.Spec.NodeMetadata.ForbiddenAnnotations.Regex,
	); err != nil {
		return ad.Deny(err.Error())
	}

	if err := h.validateRegex(
		"spec.nodeMetadata.forbiddenLabels.regex",
		config.Spec.NodeMetadata.ForbiddenLabels.Regex,
	); err != nil {
		return ad.Deny(err.Error())
	}

	return nil
}

func (h *validationHandler) validateRegex(fieldPath string, value string) error {
	if strings.TrimSpace(value) == "" {
		return nil
	}

	if _, _, err := h.regexCache.GetOrCompile(runtime.ExpressionRegex{
		Expression: value,
		Negate:     false,
	}); err != nil {
		return fmt.Errorf(
			"%s %q is not a valid regular expression: %w",
			fieldPath,
			value,
			err,
		)
	}

	return nil
}
