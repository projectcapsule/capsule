// Copyright 2020-2023 Project Capsule Authors.
// SPDX-License-Identifier: Apache-2.0

package route

import (
	capsulewebhook "github.com/projectcapsule/capsule/pkg/webhook"
)

type namespaceCordoning struct {
	handlers []capsulewebhook.Handler
}

func NamespaceCordoning(handlers ...capsulewebhook.Handler) capsulewebhook.Webhook {
	return &namespaceCordoning{handlers: handlers}
}

func (w *namespaceCordoning) GetHandlers() []capsulewebhook.Handler {
	return w.handlers
}

func (w *namespaceCordoning) GetPath() string {
	return "/tenant-namespace-cordoning"
}
