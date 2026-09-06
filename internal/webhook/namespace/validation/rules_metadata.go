// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package validation

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/internal/cache"
	genericvalidation "github.com/projectcapsule/capsule/internal/webhook/rules/generic/validation"
	"github.com/projectcapsule/capsule/pkg/ruleengine"
	"github.com/projectcapsule/capsule/pkg/runtime/configuration"
	"github.com/projectcapsule/capsule/pkg/runtime/events"
	"github.com/projectcapsule/capsule/pkg/runtime/handlers"
	"github.com/projectcapsule/capsule/pkg/tenant"
	"github.com/projectcapsule/capsule/pkg/users"
)

type rulesMetadataHandler struct {
	generic       handlers.TypedHandlerWithTenantWithRuleset[*metav1.PartialObjectMetadata]
	configuration configuration.Configuration
}

func RulesMetadataHandler(regexCache *cache.RegexCache, cfg configuration.Configuration) handlers.TypedHandlerWithTenantUser[*corev1.Namespace] {
	return &rulesMetadataHandler{generic: genericvalidation.GenericRules(regexCache), configuration: cfg}
}

func (h *rulesMetadataHandler) OnCreate(
	c client.Client,
	reader client.Reader,
	_ users.AdmissionUser,
	ns *corev1.Namespace,
	decoder admission.Decoder,
	recorder events.EventRecorder,
	tnt *capsulev1beta2.Tenant,
) handlers.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		bodies, err := tenant.BuildNamespaceRuleBodyStatus(c.Scheme(), ns, tnt)
		if err != nil {
			return handlers.ErroredResponse(err)
		}

		bodies, err = ruleengine.FilterNamespaceRulesByAudience(h.configuration, tnt, req, bodies)
		if err != nil {
			return handlers.ErroredResponse(err)
		}

		return h.generic.OnCreate(c, reader, partialNamespaceMetadata(ns), decoder, recorder, tnt, bodies)(ctx, req)
	}
}

func (h *rulesMetadataHandler) OnUpdate(
	c client.Client,
	reader client.Reader,
	_ users.AdmissionUser,
	ns *corev1.Namespace,
	old *corev1.Namespace,
	decoder admission.Decoder,
	recorder events.EventRecorder,
	tnt *capsulev1beta2.Tenant,
) handlers.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		bodies, err := tenant.BuildNamespaceRuleBodyStatus(c.Scheme(), ns, tnt)
		if err != nil {
			return handlers.ErroredResponse(err)
		}

		bodies, err = ruleengine.FilterNamespaceRulesByAudience(h.configuration, tnt, req, bodies)
		if err != nil {
			return handlers.ErroredResponse(err)
		}

		return h.generic.OnUpdate(
			c,
			reader,
			partialNamespaceMetadata(old),
			partialNamespaceMetadata(ns),
			decoder,
			recorder,
			tnt,
			bodies,
		)(ctx, req)
	}
}

func (h *rulesMetadataHandler) OnDelete(
	client.Client,
	client.Reader,
	users.AdmissionUser,
	*corev1.Namespace,
	admission.Decoder,
	events.EventRecorder,
	*capsulev1beta2.Tenant,
) handlers.Func {
	return func(context.Context, admission.Request) *admission.Response { return nil }
}

func partialNamespaceMetadata(ns *corev1.Namespace) *metav1.PartialObjectMetadata {
	if ns == nil {
		return nil
	}

	return &metav1.PartialObjectMetadata{ObjectMeta: *ns.ObjectMeta.DeepCopy()}
}
