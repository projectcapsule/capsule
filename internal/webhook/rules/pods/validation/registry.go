// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package validation

import (
	"fmt"
	"sort"
	"strings"

	corev1 "k8s.io/api/core/v1"

	"github.com/projectcapsule/capsule/internal/cache"
	apirules "github.com/projectcapsule/capsule/pkg/api/rules"
	"github.com/projectcapsule/capsule/pkg/api/runtime"
	ruleengine "github.com/projectcapsule/capsule/pkg/ruleengine"
	"github.com/projectcapsule/capsule/pkg/runtime/events"
)

type registryReference struct {
	Target     apirules.WorkloadValidationTarget
	Reference  string
	PullPolicy corev1.PullPolicy
	Path       string
}

type registryRuleSet struct {
	Registries []apirules.OCIRegistry
}

func (h *podRules) validateRegistries(
	pod *corev1.Pod,
	enforceBodies []*apirules.NamespaceRuleEnforceBody,
) (*ruleengine.Evaluation, error) {
	if h.registryCache == nil {
		return nil, fmt.Errorf("registry rule set cache is nil")
	}

	if pod == nil || len(enforceBodies) == 0 {
		return nil, nil
	}

	out := &ruleengine.Evaluation{}

	for _, ref := range registryReferencesFromPod(pod) {
		if strings.TrimSpace(ref.Reference) == "" {
			out.Blocking = &ruleengine.Decision{
				SetName:     "registry",
				EventReason: events.ReasonForbiddenContainerRegistry,
				Action:      apirules.ActionTypeDeny,
				Value: ruleengine.Value{
					Value: ref.Reference,
					Path:  ref.Path,
				},
				Message: fmt.Sprintf("%s has empty reference", ref.Path),
			}

			return out, nil
		}

		evaluation, err := h.evaluateRegistryReference(ref, enforceBodies)
		if err != nil {
			return out, err
		}

		out.Append(evaluation)

		if evaluation == nil {
			continue
		}

		//nolint:nilerr
		if err := evaluation.BlockingError(); err != nil {
			return out, nil
		}

		// Pull policy constraints are enforced only after the final registry
		// decision is an explicit allow. Audit rules do not influence this.
		if evaluation.Final == nil || evaluation.Final.Action != apirules.ActionTypeAllow {
			continue
		}

		matched, _ := evaluation.Final.MatchedValue.(*cache.CompiledRule)
		if blocking := registryPullPolicyDecision(ref, matched); blocking != nil {
			out.Blocking = blocking

			return out, nil
		}
	}

	return out, nil
}

func (h *podRules) evaluateRegistryReference(
	ref registryReference,
	enforceBodies []*apirules.NamespaceRuleEnforceBody,
) (*ruleengine.Evaluation, error) {
	return ruleengine.EvaluateEnforce[registryRuleSet](
		ref,
		enforceBodies,
		ruleengine.Set[registryRuleSet, registryReference]{
			Name:        "registry",
			EventReason: events.ReasonForbiddenContainerRegistry,
			Values: func(ref registryReference) []ruleengine.Value {
				return []ruleengine.Value{
					{
						Value: strings.TrimSpace(ref.Reference),
						Path:  ref.Path,
					},
				}
			},
			Rules: func(enforce *apirules.NamespaceRuleEnforceBody) []registryRuleSet {
				if enforce == nil {
					return nil
				}

				if len(enforce.Workloads.Registries) == 0 {
					return nil
				}

				if !enforce.WorkloadTargetsAny(ref.Target) {
					return nil
				}

				return []registryRuleSet{
					{
						Registries: enforce.Workloads.Registries,
					},
				}
			},
			Matches: func(set registryRuleSet, value ruleengine.Value) (ruleengine.Match, error) {
				rs, _, err := h.registryCache.GetOrBuild(set.Registries)
				if err != nil {
					return ruleengine.Match{}, err
				}

				if rs == nil {
					return ruleengine.Match{}, nil
				}

				// Match by OCI reference only.
				// Pull policy is validated after the final allow decision wins.
				matched, err := h.registryCache.MatchReference(rs, value.Value)
				if err != nil {
					return ruleengine.Match{}, err
				}

				if matched == nil {
					return ruleengine.Match{}, nil
				}

				return ruleengine.Match{
					Matched:      true,
					MatchedValue: matched,
				}, nil
			},
			Message:            registryDecisionMessage,
			RuleDescription:    describeRegistryRuleSet,
			AllowedDescription: "Allowed registries",
		},
	)
}

func describeRegistryRuleSet(rule registryRuleSet) string {
	if len(rule.Registries) == 0 {
		return ""
	}

	parts := make([]string, 0, len(rule.Registries))

	for _, registry := range rule.Registries {
		description := strings.TrimSpace(
			runtime.DescribeExpressionMatch(registry.ExpressionMatch),
		)
		if description == "" {
			continue
		}

		parts = append(parts, description)
	}

	return strings.Join(parts, ", ")
}

func registryReferencesFromPod(pod *corev1.Pod) []registryReference {
	if pod == nil {
		return nil
	}

	refs := make([]registryReference, 0,
		len(pod.Spec.InitContainers)+
			len(pod.Spec.Containers)+
			len(pod.Spec.EphemeralContainers)+
			len(pod.Spec.Volumes),
	)

	for i := range pod.Spec.InitContainers {
		c := pod.Spec.InitContainers[i]

		refs = append(refs, registryReference{
			Target:     apirules.ValidateInitContainers,
			Reference:  c.Image,
			PullPolicy: c.ImagePullPolicy,
			Path:       fmt.Sprintf("initContainers[%d]", i),
		})
	}

	for i := range pod.Spec.Containers {
		c := pod.Spec.Containers[i]

		refs = append(refs, registryReference{
			Target:     apirules.ValidateContainers,
			Reference:  c.Image,
			PullPolicy: c.ImagePullPolicy,
			Path:       fmt.Sprintf("containers[%d]", i),
		})
	}

	for i := range pod.Spec.EphemeralContainers {
		c := pod.Spec.EphemeralContainers[i]

		refs = append(refs, registryReference{
			Target:     apirules.ValidateEphemeralContainers,
			Reference:  c.Image,
			PullPolicy: c.ImagePullPolicy,
			Path:       fmt.Sprintf("ephemeralContainers[%d]", i),
		})
	}

	for i := range pod.Spec.Volumes {
		v := pod.Spec.Volumes[i]
		if v.Image == nil {
			continue
		}

		refs = append(refs, registryReference{
			Target:     apirules.ValidateVolumes,
			Reference:  v.Image.Reference,
			PullPolicy: v.Image.PullPolicy,
			Path:       fmt.Sprintf("volumes[%d](%s)", i, v.Name),
		})
	}

	return refs
}

func registryDecisionMessage(
	action apirules.ActionType,
	value ruleengine.Value,
	matchedValue any,
) string {
	matched, _ := matchedValue.(*cache.CompiledRule)

	rule := "<unknown>"
	if matched != nil {
		rule = registryRuleDescription(matched)
	}

	switch action {
	case apirules.ActionTypeAudit:
		return fmt.Sprintf(
			"%s reference %q matched audit registry rule %q",
			value.Path,
			value.Value,
			rule,
		)

	case apirules.ActionTypeDeny:
		return fmt.Sprintf(
			"%s reference %q is denied by registry rule %q",
			value.Path,
			value.Value,
			rule,
		)

	case apirules.ActionTypeAllow:
		return fmt.Sprintf(
			"%s reference %q is allowed by registry rule %q",
			value.Path,
			value.Value,
			rule,
		)

	default:
		return fmt.Sprintf(
			"%s reference %q matched registry rule %q with action %q",
			value.Path,
			value.Value,
			rule,
			action,
		)
	}
}

func registryRuleDescription(matched *cache.CompiledRule) string {
	if matched == nil {
		return "<unknown>"
	}

	parts := make([]string, 0, 2)

	if len(matched.Match.Exact) > 0 {
		exact := append([]string(nil), matched.Match.Exact...)
		sort.Strings(exact)

		parts = append(parts, "exact="+strings.Join(exact, ","))
	}

	if matched.Match.Expression != "" {
		if matched.Match.Negate {
			parts = append(parts, "exp="+matched.Match.Expression+",negate=true")
		} else {
			parts = append(parts, "exp="+matched.Match.Expression)
		}
	}

	if len(parts) == 0 {
		return "<unknown>"
	}

	return strings.Join(parts, ";")
}

func registryPullPolicyDecision(
	ref registryReference,
	matched *cache.CompiledRule,
) *ruleengine.Decision {
	if matched == nil || len(matched.AllowedPolicy) == 0 {
		return nil
	}

	allowed := formatAllowedPullPolicies(matched.AllowedPolicy)

	if ref.PullPolicy == "" {
		return &ruleengine.Decision{
			SetName:     "registry",
			EventReason: events.ReasonForbiddenPullPolicy,
			Action:      apirules.ActionTypeDeny,
			Value: ruleengine.Value{
				Value: ref.Reference,
				Path:  ref.Path,
			},
			MatchedValue: matched,
			Message: fmt.Sprintf(
				"%s reference %q must explicitly set pullPolicy (allowed: %s)",
				ref.Path,
				ref.Reference,
				allowed,
			),
		}
	}

	if _, ok := matched.AllowedPolicy[ref.PullPolicy]; !ok {
		return &ruleengine.Decision{
			SetName:     "registry",
			EventReason: events.ReasonForbiddenPullPolicy,
			Action:      apirules.ActionTypeDeny,
			Value: ruleengine.Value{
				Value: ref.Reference,
				Path:  ref.Path,
			},
			MatchedValue: matched,
			Message: fmt.Sprintf(
				"%s reference %q uses pullPolicy=%s which is not allowed (allowed: %s)",
				ref.Path,
				ref.Reference,
				ref.PullPolicy,
				allowed,
			),
		}
	}

	return nil
}

func formatAllowedPullPolicies(policies map[corev1.PullPolicy]struct{}) string {
	if len(policies) == 0 {
		return ""
	}

	out := make([]string, 0, len(policies))
	for policy := range policies {
		out = append(out, string(policy))
	}

	sort.Strings(out)

	return strings.Join(out, ", ")
}
