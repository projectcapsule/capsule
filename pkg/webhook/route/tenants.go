// Copyright 2020-2023 Project Capsule Authors.
// SPDX-License-Identifier: Apache-2.0

package route

import (
	capsulewebhook "github.com/projectcapsule/capsule/pkg/webhook"
)

type tenant struct {
	handlers []capsulewebhook.Handler
}

func Tenant(handler ...capsulewebhook.Handler) capsulewebhook.Webhook {
	return &tenant{handlers: handler}
}

func (w *tenant) GetHandlers() []capsulewebhook.Handler {
	return w.handlers
}

func (w *tenant) GetPath() string {
	return "/tenants"
}
