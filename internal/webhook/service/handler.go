// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package service

import (
	corev1 "k8s.io/api/core/v1"

	"github.com/projectcapsule/capsule/pkg/runtime/handlers"
)

func Handler(handler ...handlers.TypedHandlerWithTenant[*corev1.Service]) handlers.Handler {
	return &handlers.TypedTenantHandler[*corev1.Service]{
		Factory: func() *corev1.Service {
			return &corev1.Service{}
		},
		Handlers: handler,
	}
}
