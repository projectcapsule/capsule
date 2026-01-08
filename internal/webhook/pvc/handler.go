// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package pvc

import (
	corev1 "k8s.io/api/core/v1"

	"github.com/projectcapsule/capsule/internal/webhook"
	"github.com/projectcapsule/capsule/internal/webhook/utils"
)

func Handler(handlers ...webhook.TypedHandlerWithTenant[*corev1.PersistentVolumeClaim]) webhook.Handler {
	return &utils.TypedTenantHandler[*corev1.PersistentVolumeClaim]{
		Factory: func() *corev1.PersistentVolumeClaim {
			return &corev1.PersistentVolumeClaim{}
		},
		Handlers: handlers,
	}
}
