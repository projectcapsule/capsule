// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package route

import "github.com/projectcapsule/capsule/pkg/runtime/handlers"

type defaults struct {
	handlers []handlers.Handler
}

func Defaults(handler ...handlers.Handler) handlers.Webhook {
	return &defaults{handlers: handler}
}

func (w *defaults) GetHandlers() []handlers.Handler {
	return w.handlers
}

func (w *defaults) GetPath() string {
	return "/defaults"
}
