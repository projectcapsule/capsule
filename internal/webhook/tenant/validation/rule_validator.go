// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package validation

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	apiequality "k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	k8smeta "k8s.io/apimachinery/pkg/api/meta"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/rules"
	"github.com/projectcapsule/capsule/pkg/ruleengine"
	ad "github.com/projectcapsule/capsule/pkg/runtime/admission"
	"github.com/projectcapsule/capsule/pkg/runtime/configuration"
	"github.com/projectcapsule/capsule/pkg/runtime/events"
	"github.com/projectcapsule/capsule/pkg/runtime/handlers"
	runtimequota "github.com/projectcapsule/capsule/pkg/runtime/quota"
	tenantutils "github.com/projectcapsule/capsule/pkg/tenant"
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
	return func(context.Context, admission.Request) *admission.Response {
		if err := h.handle(tnt); err != nil {
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
	c client.Client,
	reader client.Reader,
	tnt *capsulev1beta2.Tenant,
	old *capsulev1beta2.Tenant,
	decoder admission.Decoder,
	_ events.EventRecorder,
) handlers.Func {
	return func(ctx context.Context, _ admission.Request) *admission.Response {
		if response := h.handle(tnt); response != nil {
			return response
		}

		if reader == nil {
			reader = c
		}

		return validateRuleQuotaUpdates(ctx, reader, tnt, old)
	}
}

func validateRuleQuotaUpdates(
	ctx context.Context,
	reader client.Reader,
	tnt *capsulev1beta2.Tenant,
	old *capsulev1beta2.Tenant,
) *admission.Response {
	if reader == nil || tnt == nil || old == nil {
		return nil
	}

	oldHard := make(map[string]corev1.ResourceList)

	for _, rule := range old.Spec.Rules {
		if rule == nil || rule.NamespaceRuleBodyNamespace == nil {
			continue
		}

		for _, quota := range rule.Quota {
			oldHard[quota.Name] = quota.Hard
		}
	}

	for ruleIndex, rule := range tnt.Spec.Rules {
		if rule == nil || rule.NamespaceRuleBodyNamespace == nil {
			continue
		}

		for quotaIndex, quota := range rule.Quota {
			previous, existed := oldHard[quota.Name]
			if !existed || apiequality.Semantic.DeepEqual(previous, quota.Hard) {
				continue
			}

			globalQuota := &capsulev1beta2.GlobalResourceQuota{}
			if err := reader.Get(ctx, client.ObjectKey{
				Name: tenantutils.RuleGlobalResourceQuotaName(tnt, quota.Name),
			}, globalQuota); err != nil {
				if apierrors.IsNotFound(err) {
					continue
				}

				return ad.ErroredResponse(fmt.Errorf(
					"cannot validate rules[%d].quota[%d] against its GlobalResourceQuota: %w",
					ruleIndex,
					quotaIndex,
					err,
				))
			}

			path := fmt.Sprintf("rules[%d].quota[%d].hard", ruleIndex, quotaIndex)
			if err := runtimequota.ValidateHardLimit(path, quota.Hard, globalQuota.Status.Total.Used); err != nil {
				return ad.Deny(err.Error())
			}

			ledger := &capsulev1beta2.QuantityLedger{}

			err := reader.Get(ctx, client.ObjectKey{
				Namespace: configuration.ControllerNamespace(),
				Name:      globalQuota.GetLedgerName(),
			}, ledger)
			if apierrors.IsNotFound(err) {
				continue
			}

			if err != nil {
				return ad.ErroredResponse(fmt.Errorf(
					"cannot validate rules[%d].quota[%d] against its QuantityLedger: %w",
					ruleIndex,
					quotaIndex,
					err,
				))
			}

			if ledger.Spec.TargetRef.UID != globalQuota.UID || ledger.Status.ResourceQuota == nil {
				continue
			}

			if err := runtimequota.ValidateHardLimit(
				path,
				quota.Hard,
				ledger.Status.ResourceQuota.Allocated,
			); err != nil {
				return ad.Deny(err.Error())
			}
		}
	}

	return nil
}

func (h *RuleValidationHandler) handle(
	tnt *capsulev1beta2.Tenant,
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
		if body.Enforce == nil && len(body.Quota) == 0 {
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

	for ruleIndex, rule := range tnt.Spec.Rules {
		if rule == nil || rule.NamespaceRuleBodyNamespace == nil {
			continue
		}

		for quotaIndex, quota := range rule.Quota {
			if err := tenantutils.ValidateRuleGlobalResourceQuotaName(tnt, quota.Name); err != nil {
				return ad.Deny(fmt.Sprintf("rules[%d].quota[%d]: %v", ruleIndex, quotaIndex, err))
			}
		}
	}

	return nil
}
