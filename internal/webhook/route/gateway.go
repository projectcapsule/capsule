// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package route

import "github.com/projectcapsule/capsule/pkg/runtime/handlers"

type gateway struct {
	handlers []handlers.Handler
}

func Gateway(handler ...handlers.Handler) handlers.Webhook {
	return &gateway{handlers: handler}
}

func (w *gateway) GetHandlers() []handlers.Handler {
	return w.handlers
}

func (w *gateway) GetPath() string {
	return "/gateways"
}
