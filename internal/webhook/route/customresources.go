// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package route

import "github.com/projectcapsule/capsule/pkg/runtime/handlers"

type customResourcesHandler struct {
	handlers []handlers.Handler
}

func CustomResources(handlers ...handlers.Handler) handlers.Webhook {
	return &customResourcesHandler{handlers: handlers}
}

func (w *customResourcesHandler) GetHandlers() []handlers.Handler {
	return w.handlers
}

func (w *customResourcesHandler) GetPath() string {
	return "/customresources"
}
