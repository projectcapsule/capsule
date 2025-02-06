// Copyright 2020-2023 Project Capsule Authors.
// SPDX-License-Identifier: Apache-2.0

package route

import (
	capsulewebhook "github.com/projectcapsule/capsule/pkg/webhook"
)

type webhook struct {
	handlers []capsulewebhook.Handler
}

func OwnerReference(handlers ...capsulewebhook.Handler) capsulewebhook.Webhook {
	return &webhook{handlers: handlers}
}

func (w *webhook) GetHandlers() []capsulewebhook.Handler {
	return w.handlers
}

func (w *webhook) GetPath() string {
	return "/namespace-owner-reference"
}
