// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package validation

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/internal/cache"
	"github.com/projectcapsule/capsule/internal/webhook/pvc"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/api/rules"
	"github.com/projectcapsule/capsule/pkg/ruleengine"
	"github.com/projectcapsule/capsule/pkg/runtime/configuration"
	"github.com/projectcapsule/capsule/pkg/runtime/events"
	"github.com/projectcapsule/capsule/pkg/tenant"
)

type volumeRules struct {
	configuration configuration.Configuration
	selectors     *cache.LabelSelectorCache
	compiler      ruleengine.ConditionCompiler
}

func VolumeRules(cfg configuration.Configuration, selectors *cache.LabelSelectorCache, compiler ruleengine.ConditionCompiler) pvc.VolumeAccess {
	return &volumeRules{configuration: cfg, selectors: selectors, compiler: compiler}
}

func (h *volumeRules) Allows(ctx context.Context, c client.Client, reader client.Reader, req admission.Request, claim *corev1.PersistentVolumeClaim, volume *corev1.PersistentVolume, tnt *capsulev1beta2.Tenant, recorder events.EventRecorder) (bool, error) {
	// Ownership is a hard boundary, even for a wildcard selector or CEL allow.
	if claim == nil || volume == nil || tnt == nil || volume.DeletionTimestamp != nil {
		return false, nil
	}

	if _, owned := volume.Labels[meta.TenantLabel]; owned {
		return false, nil
	}

	// Avoid ruleset reads on installations without this opt-in feature. This
	// also prevents a removed Tenant rule from granting through stale status.
	configured := false

	for _, rule := range tnt.Spec.Rules {
		if rule != nil && rule.NamespaceRuleBodyNamespace != nil && rule.Enforce != nil && len(rule.Enforce.Storage.Volumes) > 0 {
			configured = true

			break
		}
	}

	if !configured {
		return false, nil
	}

	bodies, err := tenant.GetNamespaceRuleBodies(ctx, reader, c.Scheme(), claim.Namespace, tnt)
	if err != nil {
		return false, err
	}

	bodies, err = ruleengine.FilterNamespaceRulesByAudience(ctx, c, h.configuration, tnt, req, bodies)
	if err != nil {
		return false, err
	}

	enforce, err := ruleengine.FilterEnforcementConditions(ctx,
		ruleengine.NewConditionEvaluator(h.compiler, req.AdmissionRequest).WithVolume(volume),
		claim, ruleengine.EnforceBodiesFromNamespaceRules(bodies),
		func(body *rules.NamespaceRuleEnforceBody) bool { return len(body.Storage.Volumes) > 0 })
	if err != nil {
		return false, fmt.Errorf("volume access conditions: %w", err)
	}

	evaluation, err := ruleengine.EvaluateEnforce(volume, enforce, ruleengine.Set[rules.PersistentVolumeMatch, *corev1.PersistentVolume]{
		Name: "additional PersistentVolume access",
		Values: func(pv *corev1.PersistentVolume) []ruleengine.Value {
			return []ruleengine.Value{{Value: pv.Name, Path: "spec.volumeName"}}
		},
		Rules: func(body *rules.NamespaceRuleEnforceBody) []rules.PersistentVolumeMatch { return body.Storage.Volumes },
		Matches: func(match rules.PersistentVolumeMatch, _ ruleengine.Value) (ruleengine.Match, error) {
			if match.Selector == nil || h.selectors == nil {
				return ruleengine.Match{}, fmt.Errorf("volume selector or selector compiler is unavailable")
			}

			selector, err := h.selectors.GetOrCompile(match.Selector)
			if err != nil {
				return ruleengine.Match{}, err
			}

			return ruleengine.Match{Matched: selector.Matches(labels.Set(volume.Labels)), MatchedValue: match.Name}, nil
		},
		RuleDescription: func(match rules.PersistentVolumeMatch) string { return match.Name },
	})
	if err != nil {
		return false, err
	}

	if recorder != nil {
		for _, audit := range evaluation.Audits {
			recorder.LabeledEvent(claim, corev1.EventTypeNormal, events.ReasonNamespaceRuleAudit, events.ActionRuleAudit, audit.Message).
				WithRelated(tnt).WithTenantLabel(tnt).WithRequestAnnotations(req).Emit(ctx)
		}
	}

	return evaluation.Final != nil && evaluation.Final.Action == rules.ActionTypeAllow && evaluation.Blocking == nil, nil
}
