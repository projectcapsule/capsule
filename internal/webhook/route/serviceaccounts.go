// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package route

import "github.com/projectcapsule/capsule/pkg/runtime/handlers"

type serviceaccounts struct {
	handlers []handlers.Handler
}

func ServiceAccounts(handler ...handlers.Handler) handlers.Webhook {
	return &serviceaccounts{handlers: handler}
}

func (w *serviceaccounts) GetHandlers() []handlers.Handler {
	return w.handlers
}

func (w *serviceaccounts) GetPath() string {
	return "/serviceaccounts"
}
