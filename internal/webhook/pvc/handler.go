// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package pvc

import (
	corev1 "k8s.io/api/core/v1"

	"github.com/projectcapsule/capsule/pkg/runtime/handlers"
)

func Handler(handler ...handlers.TypedHandlerWithTenant[*corev1.PersistentVolumeClaim]) handlers.Handler {
	return &handlers.TypedTenantHandler[*corev1.PersistentVolumeClaim]{
		Factory: func() *corev1.PersistentVolumeClaim {
			return &corev1.PersistentVolumeClaim{}
		},
		Handlers: handler,
	}
}
