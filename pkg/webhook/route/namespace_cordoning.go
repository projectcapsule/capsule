// Copyright 2020-2023 Project Capsule Authors.
// SPDX-License-Identifier: Apache-2.0

package route

import (
	capsulewebhook "github.com/projectcapsule/capsule/pkg/webhook"
)

type namespaceCordoningWebhook struct {
	handlers []capsulewebhook.Handler
}

func NamespaceCordoning(handlers ...capsulewebhook.Handler) capsulewebhook.Webhook {
	return &namespaceCordoningWebhook{handlers: handlers}
}

func (w *namespaceCordoningWebhook) GetHandlers() []capsulewebhook.Handler {
	return w.handlers
}

func (w *namespaceCordoningWebhook) GetPath() string {
	return "/namespace-cordoning"
}
