// Copyright 2020-2023 Project Capsule Authors.
// SPDX-License-Identifier: Apache-2.0

package route

import (
	capsulewebhook "github.com/projectcapsule/capsule/pkg/webhook"
)

type namespacePatching struct {
	handlers []capsulewebhook.Handler
}

func NamespacePatching(handlers ...capsulewebhook.Handler) capsulewebhook.Webhook {
	return &namespacePatching{handlers: handlers}
}

func (w *namespacePatching) GetHandlers() []capsulewebhook.Handler {
	return w.handlers
}

func (w *namespacePatching) GetPath() string {
	return "/namespace-patching"
}
