// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package route

import "github.com/projectcapsule/capsule/pkg/runtime/handlers"

type service struct {
	handlers []handlers.Handler
}

func Service(handler ...handlers.Handler) handlers.Webhook {
	return &service{handlers: handler}
}

func (w *service) GetHandlers() []handlers.Handler {
	return w.handlers
}

func (w *service) GetPath() string {
	return "/services"
}
