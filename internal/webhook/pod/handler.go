// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package pod

import (
	corev1 "k8s.io/api/core/v1"

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
