// Copyright 2020-2023 Project Capsule Authors.
// SPDX-License-Identifier: Apache-2.0

package route

import (
	capsulewebhook "github.com/projectcapsule/capsule/pkg/webhook"
)

type namespaceValidate struct {
	handlers []capsulewebhook.Handler
}

type namespacePatch struct {
	handlers []capsulewebhook.Handler
}

func NamespaceValidate(handler ...capsulewebhook.Handler) capsulewebhook.Webhook {
	return &namespaceValidate{handlers: handler}
}

func (w *namespaceValidate) GetHandlers() []capsulewebhook.Handler {
	return w.handlers
}

func (w *namespaceValidate) GetPath() string {
	return "/namespace-validate"
}

func NamespacePatch(handler ...capsulewebhook.Handler) capsulewebhook.Webhook {
	return &namespacePatch{handlers: handler}
}

func (w *namespacePatch) GetHandlers() []capsulewebhook.Handler {
	return w.handlers
}

func (w *namespacePatch) GetPath() string {
	return "/namespace-patch"
}
