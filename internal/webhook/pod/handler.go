// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package pod

import (
	corev1 "k8s.io/api/core/v1"

	apirules "github.com/projectcapsule/capsule/pkg/api/rules"
	"github.com/projectcapsule/capsule/pkg/rules"
	"github.com/projectcapsule/capsule/pkg/runtime/handlers"
)

func Handler(handler ...handlers.TypedHandlerWithTenantWithRuleset[*corev1.Pod]) handlers.Handler {
	return &handlers.TypedTenantWithRulesetHandler[*corev1.Pod]{
		Factory: func() *corev1.Pod {
			return &corev1.Pod{}
		},
		Handlers: handler,
	}
}

type podRuleSet[R any] = rules.Set[R, *corev1.Pod]

func evaluatePodRules[R any](
	pod *corev1.Pod,
	enforceBodies []*apirules.NamespaceRuleEnforceBody,
	set podRuleSet[R],
) (*rules.Evaluation, error) {
	if pod == nil || len(enforceBodies) == 0 {
		return nil, nil
	}

	return rules.EvaluateEnforce(
		pod,
		enforceBodies,
		set,
	)
}
