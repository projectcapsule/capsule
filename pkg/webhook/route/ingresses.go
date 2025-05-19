// Copyright 2020-2023 Project Capsule Authors.
// SPDX-License-Identifier: Apache-2.0

package route

import (
	capsulewebhook "github.com/projectcapsule/capsule/pkg/webhook"
)

type ingress struct {
	handlers []capsulewebhook.Handler
}

func Ingress(handler ...capsulewebhook.Handler) capsulewebhook.Webhook {
	return &ingress{handlers: handler}
}

func (w *ingress) GetHandlers() []capsulewebhook.Handler {
	return w.handlers
}

func (w *ingress) GetPath() string {
	return "/ingresses"
}
