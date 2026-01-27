// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package route

import "github.com/projectcapsule/capsule/pkg/runtime/handlers"

type networkPolicy struct {
	handlers []handlers.Handler
}

func NetworkPolicy(handler ...handlers.Handler) handlers.Webhook {
	return &networkPolicy{handlers: handler}
}

func (w *networkPolicy) GetHandlers() []handlers.Handler {
	return w.handlers
}

func (w *networkPolicy) GetPath() string {
	return "/networkpolicies"
}
