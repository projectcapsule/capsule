// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package route

import "github.com/projectcapsule/capsule/pkg/runtime/handlers"

type deviceClass struct {
	handlers []handlers.Handler
}

func DeviceClass(handler ...handlers.Handler) handlers.Webhook {
	return &deviceClass{handlers: handler}
}

func (w *deviceClass) GetHandlers() []handlers.Handler {
	return w.handlers
}

func (w *deviceClass) GetPath() string {
	return "/devices/validating"
}
