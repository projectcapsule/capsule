// Copyright 2020-2025 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package route

import (
	capsulewebhook "github.com/projectcapsule/capsule/internal/webhook"
)

type namespace struct {
	handlers []capsulewebhook.Handler
}

func NamespaceValidation(handler ...capsulewebhook.Handler) capsulewebhook.Webhook {
	return &namespace{handlers: handler}
}

func (w *namespace) GetHandlers() []capsulewebhook.Handler {
	return w.handlers
}

func (w *namespace) GetPath() string {
	return "/namespaces"
}

type namespacePatch struct {
	handlers []capsulewebhook.Handler
}

func NamespaceMutation(handlers ...capsulewebhook.Handler) capsulewebhook.Webhook {
	return &namespacePatch{handlers: handlers}
}

func (w *namespacePatch) GetHandlers() []capsulewebhook.Handler {
	return w.handlers
}

func (w *namespacePatch) GetPath() string {
	return "/namespace-patch"
}
