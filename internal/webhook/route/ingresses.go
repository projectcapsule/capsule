// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package route

import "github.com/projectcapsule/capsule/pkg/runtime/handlers"

type ingress struct {
	handlers []handlers.Handler
}

func Ingress(handler ...handlers.Handler) handlers.Webhook {
	return &ingress{handlers: handler}
}

func (w *ingress) GetHandlers() []handlers.Handler {
	return w.handlers
}

func (w *ingress) GetPath() string {
	return "/ingresses"
}
