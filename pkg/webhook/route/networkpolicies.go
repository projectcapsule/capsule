// Copyright 2020-2023 Project Capsule Authors.
// SPDX-License-Identifier: Apache-2.0

package route

import (
	capsulewebhook "github.com/projectcapsule/capsule/pkg/webhook"
)

type networkPolicy struct {
	handlers []capsulewebhook.Handler
}

func NetworkPolicy(handler ...capsulewebhook.Handler) capsulewebhook.Webhook {
	return &networkPolicy{handlers: handler}
}

func (w *networkPolicy) GetHandlers() []capsulewebhook.Handler {
	return w.handlers
}

func (w *networkPolicy) GetPath() string {
	return "/networkpolicies"
}
