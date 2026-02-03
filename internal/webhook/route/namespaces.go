// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

//nolint:dupl
package route

import "github.com/projectcapsule/capsule/pkg/runtime/handlers"

type namespace struct {
	handlers []handlers.Handler
}

func NamespaceValidation(handler ...handlers.Handler) handlers.Webhook {
	return &namespace{handlers: handler}
}

func (w *namespace) GetHandlers() []handlers.Handler {
	return w.handlers
}

func (w *namespace) GetPath() string {
	return "/namespaces/validating"
}

type namespacePatch struct {
	handlers []handlers.Handler
}

func NamespaceMutation(handlers ...handlers.Handler) handlers.Webhook {
	return &namespacePatch{handlers: handlers}
}

func (w *namespacePatch) GetHandlers() []handlers.Handler {
	return w.handlers
}

func (w *namespacePatch) GetPath() string {
	return "/namespaces/mutating"
}
