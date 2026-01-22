// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package route

import (
	capsulewebhook "github.com/projectcapsule/capsule/internal/webhook"
)

type gateway struct {
	handlers []capsulewebhook.Handler
}

func Gateway(handler ...capsulewebhook.Handler) capsulewebhook.Webhook {
	return &gateway{handlers: handler}
}

func (w *gateway) GetHandlers() []capsulewebhook.Handler {
	return w.handlers
}

func (w *gateway) GetPath() string {
	return "/gateways"
}
