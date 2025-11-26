// Copyright 2020-2025 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package service

import (
	corev1 "k8s.io/api/core/v1"

	"github.com/projectcapsule/capsule/internal/webhook"
	"github.com/projectcapsule/capsule/internal/webhook/utils"
)

func Handler(handlers ...webhook.TypedHandlerWithTenant[*corev1.Service]) webhook.Handler {
	return &utils.TypedTenantHandler[*corev1.Service]{
		Factory: func() *corev1.Service {
			return &corev1.Service{}
		},
		Handlers: handlers,
	}
}
