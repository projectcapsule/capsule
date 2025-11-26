// Copyright 2020-2025 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package pod

import (
	corev1 "k8s.io/api/core/v1"

	"github.com/projectcapsule/capsule/internal/webhook"
	"github.com/projectcapsule/capsule/internal/webhook/utils"
)

func Handler(handlers ...webhook.TypedHandlerWithTenant[*corev1.Pod]) webhook.Handler {
	return &utils.TypedTenantHandler[*corev1.Pod]{
		Factory: func() *corev1.Pod {
			return &corev1.Pod{}
		},
		Handlers: handlers,
	}
}
