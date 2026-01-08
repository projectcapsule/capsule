// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package route

import (
	capsulewebhook "github.com/projectcapsule/capsule/internal/webhook"
)

type serviceaccounts struct {
	handlers []capsulewebhook.Handler
}

func ServiceAccounts(handler ...capsulewebhook.Handler) capsulewebhook.Webhook {
	return &serviceaccounts{handlers: handler}
}

func (w *serviceaccounts) GetHandlers() []capsulewebhook.Handler {
	return w.handlers
}

func (w *serviceaccounts) GetPath() string {
	return "/serviceaccounts"
}
