// Copyright 2020-2023 Project Capsule Authors.
// SPDX-License-Identifier: Apache-2.0

package route

import (
	capsulewebhook "github.com/projectcapsule/capsule/pkg/webhook"
)

type service struct {
	handlers []capsulewebhook.Handler
}

func Service(handler ...capsulewebhook.Handler) capsulewebhook.Webhook {
	return &service{handlers: handler}
}

func (w *service) GetHandlers() []capsulewebhook.Handler {
	return w.handlers
}

func (w *service) GetPath() string {
	return "/services"
}
