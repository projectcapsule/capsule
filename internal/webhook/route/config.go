// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package route

import (
	capsulewebhook "github.com/projectcapsule/capsule/internal/webhook"
)

type configValidating struct {
	handlers []capsulewebhook.Handler
}

func ConfigValidation(handler ...capsulewebhook.Handler) capsulewebhook.Webhook {
	return &configValidating{handlers: handler}
}

func (w *configValidating) GetHandlers() []capsulewebhook.Handler {
	return w.handlers
}

func (w *configValidating) GetPath() string {
	return "/config/validating"
}
