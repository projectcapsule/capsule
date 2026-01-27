// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package route

import "github.com/projectcapsule/capsule/pkg/runtime/handlers"

type pvc struct {
	handlers []handlers.Handler
}

func PVC(handler ...handlers.Handler) handlers.Webhook {
	return &pvc{handlers: handler}
}

func (w *pvc) GetHandlers() []handlers.Handler {
	return w.handlers
}

func (w *pvc) GetPath() string {
	return "/persistentvolumeclaims"
}
