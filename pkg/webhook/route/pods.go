// Copyright 2020-2023 Project Capsule Authors.
// SPDX-License-Identifier: Apache-2.0

package route

import (
	capsulewebhook "github.com/projectcapsule/capsule/pkg/webhook"
)

type pod struct {
	handlers []capsulewebhook.Handler
}

func Pod(handler ...capsulewebhook.Handler) capsulewebhook.Webhook {
	return &pod{handlers: handler}
}

func (w *pod) GetHandlers() []capsulewebhook.Handler {
	return w.handlers
}

func (w *pod) GetPath() string {
	return "/pods"
}
