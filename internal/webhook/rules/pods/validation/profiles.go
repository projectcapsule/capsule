// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package validation

import (
	"fmt"
	"slices"
	"strings"

	corev1 "k8s.io/api/core/v1"

	"github.com/projectcapsule/capsule/pkg/api/rules"
	"github.com/projectcapsule/capsule/pkg/ruleengine"
	"github.com/projectcapsule/capsule/pkg/runtime/events"
)

type securityProfile struct {
	kind      rules.SecurityProfileType
	localhost string
}

func seccompProfileValue(profile *corev1.SeccompProfile) securityProfile {
	if profile == nil {
		return securityProfile{}
	}

	value := securityProfile{kind: rules.SecurityProfileType(profile.Type)}
	if profile.LocalhostProfile != nil {
		value.localhost = *profile.LocalhostProfile
	}

	return value
}

func appArmorProfileValue(profile *corev1.AppArmorProfile) securityProfile {
	if profile == nil {
		return securityProfile{}
	}

	value := securityProfile{kind: rules.SecurityProfileType(profile.Type)}
	if profile.LocalhostProfile != nil {
		value.localhost = *profile.LocalhostProfile
	}

	return value
}

// Structured container profiles take precedence over legacy annotations, then
// Pod defaults. Keeping this order also covers controller templates, before
// Kubernetes has converted legacy annotations into container fields.
func effectiveAppArmorProfile(pod *corev1.Pod, name string, context *corev1.SecurityContext, fallback securityProfile) securityProfile {
	if context != nil && context.AppArmorProfile != nil {
		return appArmorProfileValue(context.AppArmorProfile)
	}

	if value, exists := pod.Annotations[corev1.DeprecatedAppArmorBetaContainerAnnotationKeyPrefix+name]; exists {
		switch {
		case value == corev1.DeprecatedAppArmorBetaProfileRuntimeDefault:
			return securityProfile{kind: rules.SecurityProfileRuntimeDefault}
		case value == corev1.DeprecatedAppArmorBetaProfileNameUnconfined:
			return securityProfile{kind: rules.SecurityProfileUnconfined}
		case strings.HasPrefix(value, corev1.DeprecatedAppArmorBetaProfileNamePrefix):
			return securityProfile{kind: rules.SecurityProfileLocalhost, localhost: strings.TrimPrefix(value, corev1.DeprecatedAppArmorBetaProfileNamePrefix)}
		default:
			// Invalid annotations must not silently satisfy a profile allow-list.
			return securityProfile{}
		}
	}

	return fallback
}

func (h *podRules) validateSeccompProfiles(pod *corev1.Pod, bodies []*rules.NamespaceRuleEnforceBody) (*ruleengine.Evaluation, error) {
	return h.validateSecurityProfiles(pod, bodies, false)
}

func (h *podRules) validateAppArmorProfiles(pod *corev1.Pod, bodies []*rules.NamespaceRuleEnforceBody) (*ruleengine.Evaluation, error) {
	return h.validateSecurityProfiles(pod, bodies, true)
}

func (h *podRules) validateSecurityProfiles(pod *corev1.Pod, bodies []*rules.NamespaceRuleEnforceBody, appArmor bool) (*ruleengine.Evaluation, error) {
	if pod == nil || (pod.Spec.OS != nil && pod.Spec.OS.Name == corev1.Windows) {
		return nil, nil
	}

	extract := func(body *rules.NamespaceRuleEnforceBody) []rules.WorkloadSecurityProfileMatch {
		return securityProfileMatches(body, appArmor)
	}

	if !slices.ContainsFunc(bodies, func(body *rules.NamespaceRuleEnforceBody) bool { return body != nil && len(extract(body)) > 0 }) {
		return nil, nil
	}

	field := "seccompProfile"
	if appArmor {
		field = "appArmorProfile"
	}
	// Only build values for locations actually selected by a profile policy.
	selected := func(target rules.WorkloadValidationTarget) bool {
		return slices.ContainsFunc(bodies, func(body *rules.NamespaceRuleEnforceBody) bool {
			return body != nil && len(extract(body)) > 0 && profileTargetMatches(body, target)
		})
	}

	podTarget, containers, initContainers, ephemeral := selected(rules.ValidatePod), selected(rules.ValidateContainers), selected(rules.ValidateInitContainers), selected(rules.ValidateEphemeralContainers)
	if !podTarget && !containers && !initContainers && !ephemeral {
		return nil, nil
	}

	fallback := securityProfile{}

	if pod.Spec.SecurityContext != nil {
		if appArmor {
			fallback = appArmorProfileValue(pod.Spec.SecurityContext.AppArmorProfile)
		} else {
			fallback = seccompProfileValue(pod.Spec.SecurityContext.SeccompProfile)
		}
	}

	out := &ruleengine.Evaluation{}

	evaluate := func(target rules.WorkloadValidationTarget, path string, profile securityProfile) error {
		result, err := h.evaluateSecurityProfile(profile, target, path, bodies, appArmor)
		out.Append(result)

		return err
	}

	if podTarget {
		if err := evaluate(rules.ValidatePod, "spec.securityContext."+field, fallback); err != nil || out.Blocking != nil {
			return out, err
		}
	}

	for _, group := range []struct {
		enabled bool
		target  rules.WorkloadValidationTarget
		path    string
		items   []corev1.Container
	}{
		{containers, rules.ValidateContainers, "containers", pod.Spec.Containers},
		{initContainers, rules.ValidateInitContainers, "initContainers", pod.Spec.InitContainers},
	} {
		if !group.enabled {
			continue
		}

		for i := range group.items {
			c := &group.items[i]
			if err := evaluate(group.target, fmt.Sprintf("spec.%s[%d].securityContext.%s", group.path, i, field), effectiveSecurityProfile(pod, c.Name, c.SecurityContext, fallback, appArmor)); err != nil || out.Blocking != nil {
				return out, err
			}
		}
	}

	if ephemeral {
		for i := range pod.Spec.EphemeralContainers {
			c := &pod.Spec.EphemeralContainers[i]
			if err := evaluate(rules.ValidateEphemeralContainers, fmt.Sprintf("spec.ephemeralContainers[%d].securityContext.%s", i, field), effectiveSecurityProfile(pod, c.Name, c.SecurityContext, fallback, appArmor)); err != nil || out.Blocking != nil {
				return out, err
			}
		}
	}

	return out, nil
}

func profileTargetMatches(body *rules.NamespaceRuleEnforceBody, target rules.WorkloadValidationTarget) bool {
	// An omitted target list checks effective container profiles, allowing a Pod
	// without a default when every container declares its own profile.
	if target == rules.ValidatePod && len(body.Workloads.Targets) == 0 {
		return false
	}

	return body.GetWorkloadTargets(target)
}

func (h *podRules) evaluateSecurityProfile(profile securityProfile, target rules.WorkloadValidationTarget, path string, bodies []*rules.NamespaceRuleEnforceBody, appArmor bool) (*ruleengine.Evaluation, error) {
	name, reason := "seccomp profile", events.ReasonForbiddenSeccompProfile
	extract := func(body *rules.NamespaceRuleEnforceBody) []rules.WorkloadSecurityProfileMatch {
		return body.Workloads.SeccompProfiles
	}

	if appArmor {
		name, reason = "AppArmor profile", events.ReasonForbiddenAppArmorProfile
		extract = func(body *rules.NamespaceRuleEnforceBody) []rules.WorkloadSecurityProfileMatch {
			return body.Workloads.AppArmorProfiles
		}
	}

	description := string(profile.kind)
	if profile.kind == "" {
		description = "Unset"
	}

	if profile.kind == rules.SecurityProfileLocalhost {
		description += ":" + profile.localhost
	}

	result, err := ruleengine.EvaluateEnforce(profile, bodies, ruleengine.Set[rules.WorkloadSecurityProfileMatch, securityProfile]{
		Name: name, EventReason: reason,
		Values: func(securityProfile) []ruleengine.Value { return []ruleengine.Value{{Value: description, Path: path}} },
		Rules: func(body *rules.NamespaceRuleEnforceBody) []rules.WorkloadSecurityProfileMatch {
			if !profileTargetMatches(body, target) {
				return nil
			}

			return extract(body)
		},
		Matches: func(match rules.WorkloadSecurityProfileMatch, _ ruleengine.Value) (ruleengine.Match, error) {
			return h.matchSecurityProfile(match, profile)
		},
		RuleDescription:    placementDescription[rules.WorkloadSecurityProfileMatch],
		AllowedDescription: "Allowed profiles",
	})

	return result, err
}

func (h *podRules) matchSecurityProfile(match rules.WorkloadSecurityProfileMatch, profile securityProfile) (ruleengine.Match, error) {
	if !slices.Contains(match.Types, profile.kind) {
		return ruleengine.Match{}, nil
	}

	if profile.kind != rules.SecurityProfileLocalhost {
		return ruleengine.Match{Matched: true}, nil
	}

	if profile.localhost == "" {
		return ruleengine.Match{}, nil
	}

	if len(match.LocalhostProfiles) == 0 {
		return ruleengine.Match{Matched: true}, nil
	}

	for _, expression := range match.LocalhostProfiles {
		matched, err := expression.MatchesWithExpressionMatcher(h.regexCache, profile.localhost)
		if err != nil || matched {
			return ruleengine.Match{Matched: matched}, err
		}
	}

	return ruleengine.Match{}, nil
}

func securityProfileMatches(body *rules.NamespaceRuleEnforceBody, appArmor bool) []rules.WorkloadSecurityProfileMatch {
	if appArmor {
		return body.Workloads.AppArmorProfiles
	}

	return body.Workloads.SeccompProfiles
}

func effectiveSecurityProfile(pod *corev1.Pod, name string, sc *corev1.SecurityContext, fallback securityProfile, appArmor bool) securityProfile {
	if sc != nil && sc.Privileged != nil && *sc.Privileged {
		return securityProfile{kind: rules.SecurityProfileUnconfined}
	}

	if appArmor {
		return effectiveAppArmorProfile(pod, name, sc, fallback)
	}

	if sc != nil && sc.SeccompProfile != nil {
		return seccompProfileValue(sc.SeccompProfile)
	}

	return fallback
}
