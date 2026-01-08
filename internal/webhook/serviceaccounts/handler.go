// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package serviceaccounts

import (
	corev1 "k8s.io/api/core/v1"

	"github.com/projectcapsule/capsule/internal/webhook"
	"github.com/projectcapsule/capsule/internal/webhook/utils"
)

func Handler(handlers ...webhook.TypedHandlerWithTenant[*corev1.ServiceAccount]) webhook.Handler {
	return &utils.TypedTenantHandler[*corev1.ServiceAccount]{
		Factory: func() *corev1.ServiceAccount {
			return &corev1.ServiceAccount{}
		},
		Handlers: handlers,
	}
}
