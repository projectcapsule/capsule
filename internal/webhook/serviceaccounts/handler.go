// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package serviceaccounts

import (
	corev1 "k8s.io/api/core/v1"

	"github.com/projectcapsule/capsule/pkg/runtime/configuration"
	"github.com/projectcapsule/capsule/pkg/runtime/handlers"
)

func Handler(cfg configuration.Configuration, handler ...handlers.TypedHandlerWithTenantUser[*corev1.ServiceAccount]) handlers.Handler {
	return &handlers.TypedTenantWithUserHandler[*corev1.ServiceAccount]{
		Factory: func() *corev1.ServiceAccount {
			return &corev1.ServiceAccount{}
		},
		Handlers:      handler,
		Configuration: cfg,
	}
}
