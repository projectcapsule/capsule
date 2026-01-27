// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package route

import (
	"github.com/projectcapsule/capsule/pkg/runtime/handlers"
)

type configValidating struct {
	handlers []handlers.Handler
}

func ConfigValidation(handler ...handlers.Handler) handlers.Webhook {
	return &configValidating{handlers: handler}
}

func (w *configValidating) GetHandlers() []handlers.Handler {
	return w.handlers
}

func (w *configValidating) GetPath() string {
	return "/config/validating"
}
