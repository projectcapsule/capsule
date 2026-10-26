// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package mutation

import (
	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	"github.com/projectcapsule/capsule/pkg/ruleengine"
	"github.com/projectcapsule/capsule/pkg/runtime/configuration"
	"github.com/projectcapsule/capsule/pkg/runtime/handlers"
)

const Path = "/rules/generic/mutating"

type genericMutating struct {
	configuration configuration.Configuration
	compiler      ruleengine.ConditionCompiler
}

func Register(cfg configuration.Configuration, compiler ruleengine.ConditionCompiler) handlers.Webhook {
	return &genericMutating{configuration: cfg, compiler: compiler}
}

func (g genericMutating) GetHandlers() []handlers.Handler {
	return []handlers.Handler{handlers.MatchingRequest(matchesMutationRequest, genericHandler(g.configuration, MetadataRules(g.compiler)))}
}

func (genericMutating) GetPath() string { return Path }

func genericHandler(cfg configuration.Configuration, handler ...handlers.TypedHandlerWithTenantWithRuleset[*unstructured.Unstructured]) handlers.Handler {
	return &handlers.TypedTenantWithRulesetHandler[*unstructured.Unstructured]{
		Factory:       func() *unstructured.Unstructured { return &unstructured.Unstructured{} },
		Handlers:      handler,
		Configuration: cfg,
	}
}

func matchesMutationRequest(req admission.Request) bool {
	// Preserve malformed-kind errors on requests that previously reached mutation.
	if req.Kind.Version == "" || req.Kind.Kind == "" || req.SubResource == "" {
		return true
	}

	return req.SubResource == "ephemeralcontainers" && req.Operation == admissionv1.Update &&
		req.Kind.Group == corev1.GroupName && req.Kind.Version == corev1.SchemeGroupVersion.Version && req.Kind.Kind == "Pod"
}
