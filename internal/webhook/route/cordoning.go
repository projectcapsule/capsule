// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package route

import "github.com/projectcapsule/capsule/pkg/runtime/handlers"

type cordoning struct {
	handlers []handlers.Handler
}

func Cordoning(handlers ...handlers.Handler) handlers.Webhook {
	return &cordoning{handlers: handlers}
}

func (w cordoning) GetPath() string {
	return "/misc/cordoning"
}

func (w cordoning) GetHandlers() []handlers.Handler {
	return w.handlers
}
