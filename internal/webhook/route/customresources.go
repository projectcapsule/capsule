// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package route

import (
	capsulewebhook "github.com/projectcapsule/capsule/internal/webhook"
)

type customResourcesHandler struct {
	handlers []capsulewebhook.Handler
}

func CustomResources(handlers ...capsulewebhook.Handler) capsulewebhook.Webhook {
	return &customResourcesHandler{handlers: handlers}
}

func (w *customResourcesHandler) GetHandlers() []capsulewebhook.Handler {
	return w.handlers
}

func (w *customResourcesHandler) GetPath() string {
	return "/customresources"
}
