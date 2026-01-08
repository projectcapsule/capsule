// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package route

import (
	capsulewebhook "github.com/projectcapsule/capsule/internal/webhook"
)

type deviceClass struct {
	handlers []capsulewebhook.Handler
}

func DeviceClass(handler ...capsulewebhook.Handler) capsulewebhook.Webhook {
	return &deviceClass{handlers: handler}
}

func (w *deviceClass) GetHandlers() []capsulewebhook.Handler {
	return w.handlers
}

func (w *deviceClass) GetPath() string {
	return "/devices"
}
