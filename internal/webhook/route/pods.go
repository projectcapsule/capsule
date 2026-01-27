// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package route

import "github.com/projectcapsule/capsule/pkg/runtime/handlers"

type pod struct {
	handlers []handlers.Handler
}

func Pod(handler ...handlers.Handler) handlers.Webhook {
	return &pod{handlers: handler}
}

func (w *pod) GetHandlers() []handlers.Handler {
	return w.handlers
}

func (w *pod) GetPath() string {
	return "/pods"
}
