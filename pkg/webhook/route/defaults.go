// Copyright 2020-2023 Project Capsule Authors.
// SPDX-License-Identifier: Apache-2.0

package route

import (
	capsulewebhook "github.com/projectcapsule/capsule/pkg/webhook"
)

type defaults struct {
	handlers []capsulewebhook.Handler
}

func Defaults(handler ...capsulewebhook.Handler) capsulewebhook.Webhook {
	return &defaults{handlers: handler}
}

func (w *defaults) GetHandlers() []capsulewebhook.Handler {
	return w.handlers
}

func (w *defaults) GetPath() string {
	return "/defaults"
}
