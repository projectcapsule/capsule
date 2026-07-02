// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package validation

import (
	"context"
	"errors"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/internal/cache"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	apirules "github.com/projectcapsule/capsule/pkg/api/rules"
	"github.com/projectcapsule/capsule/pkg/ruleengine"
	ad "github.com/projectcapsule/capsule/pkg/runtime/admission"
	"github.com/projectcapsule/capsule/pkg/runtime/events"
	"github.com/projectcapsule/capsule/pkg/runtime/handlers"
)

type genericObject = *metav1.PartialObjectMetadata

type genericRuleSet[R any] = ruleengine.Set[R, genericObject]

func evaluateGenericRules[R any](
	obj genericObject,
	enforceBodies []*apirules.NamespaceRuleEnforceBody,
	set genericRuleSet[R],
) (*ruleengine.Evaluation, error) {
	if obj == nil || len(enforceBodies) == 0 {
		return nil, nil
	}

	return ruleengine.EvaluateEnforce(
		obj,
		enforceBodies,
		set,
	)
}

type genericRuleValidator func(
	genericObject,
	schema.GroupVersionKind,
	[]*apirules.NamespaceRuleEnforceBody,
) (*ruleengine.Evaluation, error)

type genericRules struct {
	rules           []genericRuleValidator
	regexCache      *cache.RegexCache
	managedMetadata meta.ManagedMetadata
	objectSkipRules []meta.ObjectSkipRule
}

func GenericRules(
	regexCache *cache.RegexCache,
) handlers.TypedHandlerWithTenantWithRuleset[genericObject] {
	if regexCache == nil {
		regexCache = cache.NewRegexCache()
	}

	h := &genericRules{
		regexCache:      regexCache,
		managedMetadata: meta.NewManagedMetadata(nil, nil),
		objectSkipRules: meta.DefaultObjectSkipRules(),
	}

	h.rules = []genericRuleValidator{
		h.validateMetadata,
	}

	return h
}

func (h *genericRules) OnCreate(
	_ client.Client,
	_ client.Reader,
	obj genericObject,
	_ admission.Decoder,
	recorder events.EventRecorder,
	tnt *capsulev1beta2.Tenant,
	bodies []*apirules.NamespaceRuleBodyNamespace,
) handlers.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		gvk, err := groupVersionKind(req)
		if err != nil {
			return ad.Deny(err.Error())
		}

		enforceBodies := ruleengine.EnforceBodiesFromNamespaceRules(bodies)

		if err := h.validateGenericRules(ctx, req, obj, gvk, tnt, recorder, enforceBodies); err != nil {
			return ad.Deny(err.Error())
		}

		return nil
	}
}

func (h *genericRules) OnUpdate(
	_ client.Client,
	_ client.Reader,
	_ genericObject,
	obj genericObject,
	_ admission.Decoder,
	recorder events.EventRecorder,
	tnt *capsulev1beta2.Tenant,
	bodies []*apirules.NamespaceRuleBodyNamespace,
) handlers.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		gvk, err := groupVersionKind(req)
		if err != nil {
			return ad.Deny(err.Error())
		}

		enforceBodies := ruleengine.EnforceBodiesFromNamespaceRules(bodies)

		if err := h.validateGenericRules(ctx, req, obj, gvk, tnt, recorder, enforceBodies); err != nil {
			return ad.Deny(err.Error())
		}

		return nil
	}
}

func (h *genericRules) OnDelete(
	_ client.Client,
	_ client.Reader,
	_ genericObject,
	_ admission.Decoder,
	_ events.EventRecorder,
	_ *capsulev1beta2.Tenant,
	_ []*apirules.NamespaceRuleBodyNamespace,
) handlers.Func {
	return func(context.Context, admission.Request) *admission.Response {
		return nil
	}
}

func (h *genericRules) validateGenericRules(
	ctx context.Context,
	req admission.Request,
	obj genericObject,
	gvk schema.GroupVersionKind,
	tnt *capsulev1beta2.Tenant,
	recorder events.EventRecorder,
	enforceBodies []*apirules.NamespaceRuleEnforceBody,
) error {
	if obj == nil {
		return nil
	}

	obj.SetGroupVersionKind(gvk)

	if meta.ShouldSkipObjectByRules(obj, h.objectSkipRules) {
		return nil
	}

	for _, evaluate := range h.rules {
		evaluation, err := evaluate(obj, gvk, enforceBodies)
		if err != nil {
			return err
		}

		if evaluation == nil {
			continue
		}

		for _, audit := range evaluation.Audits {
			recorder.LabeledEvent(
				obj,
				corev1.EventTypeNormal,
				events.ReasonNamespaceRuleAudit,
				events.ActionRuleAudit,
				audit.Message,
			).
				WithRelated(tnt).
				WithTenantLabel(tnt).
				WithRequestAnnotations(req).
				Emit(ctx)
		}

		if err := evaluation.BlockingError(); err != nil {
			var decisionErr *ruleengine.DecisionError

			if errors.As(err, &decisionErr) && decisionErr.Decision != nil {
				recorder.LabeledEvent(
					obj,
					corev1.EventTypeWarning,
					decisionErr.Decision.EventReason,
					events.ActionValidationDenied,
					decisionErr.Decision.Message,
				).
					WithRelated(tnt).
					WithTenantLabel(tnt).
					WithRequestAnnotations(req).
					Emit(ctx)
			}

			return err
		}
	}

	return nil
}

func groupVersionKind(req admission.Request) (schema.GroupVersionKind, error) {
	gvk := schema.GroupVersionKind{
		Group:   req.Kind.Group,
		Version: req.Kind.Version,
		Kind:    req.Kind.Kind,
	}

	if gvk.Version == "" || gvk.Kind == "" {
		return schema.GroupVersionKind{}, fmt.Errorf("admission request kind is incomplete: %s", gvk.String())
	}

	return gvk, nil
}
