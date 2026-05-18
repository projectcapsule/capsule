// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package route

import (
	"github.com/projectcapsule/capsule/pkg/runtime/handlers"
)

type pvcValidating struct {
	handlers []handlers.Handler
}

func PVCValidating(handler ...handlers.Handler) handlers.Webhook {
	return &pvcValidating{handlers: handler}
}

func (w *pvcValidating) GetHandlers() []handlers.Handler {
	return w.handlers
}

func (pvcValidating) GetPath() string {
	return "/persistentvolumeclaims/validating"
}

type pvcMutating struct {
	handlers []handlers.Handler
}

func PVCMutating(handler ...handlers.Handler) handlers.Webhook {
	return &pvcMutating{handlers: handler}
}

func (w *pvcMutating) GetHandlers() []handlers.Handler {
	return w.handlers
}

func (pvcMutating) GetPath() string {
	return "/persistentvolumeclaims/mutating"
}
