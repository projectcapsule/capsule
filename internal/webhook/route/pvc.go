// Copyright 2020-2025 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package route

import (
	capsulewebhook "github.com/projectcapsule/capsule/internal/webhook"
)

type pvcValidating struct {
	handlers []capsulewebhook.Handler
}

func PVCValidating(handler ...capsulewebhook.Handler) capsulewebhook.Webhook {
	return &pvcValidating{handlers: handler}
}

func (w *pvcValidating) GetHandlers() []capsulewebhook.Handler {
	return w.handlers
}

func (pvcValidating) GetPath() string {
	return "/persistentvolumeclaims/validating"
}

type pvcMutating struct {
	handlers []capsulewebhook.Handler
}

func PVCMutating(handler ...capsulewebhook.Handler) capsulewebhook.Webhook {
	return &pvcMutating{handlers: handler}
}

func (w *pvcMutating) GetHandlers() []capsulewebhook.Handler {
	return w.handlers
}

func (pvcMutating) GetPath() string {
	return "/persistentvolumeclaims/mutating"
}
