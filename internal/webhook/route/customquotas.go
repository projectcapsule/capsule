// Copyright 2020-2025 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package route

import (
	capsulewebhook "github.com/projectcapsule/capsule/internal/webhook"
)

type customQuotasHandler struct {
	handlers []capsulewebhook.Handler
}

func CustomQuotas(handlers ...capsulewebhook.Handler) capsulewebhook.Webhook {
	return &customQuotasHandler{handlers: handlers}
}

func (w *customQuotasHandler) GetHandlers() []capsulewebhook.Handler {
	return w.handlers
}

func (w *customQuotasHandler) GetPath() string {
	return "/customquotas"
}
