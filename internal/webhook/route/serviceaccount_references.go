// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package route

import "github.com/projectcapsule/capsule/pkg/runtime/handlers"

type serviceAccountReferences struct {
	handlers []handlers.Handler
}

func ServiceAccountReferences(handler ...handlers.Handler) handlers.Webhook {
	return &serviceAccountReferences{handlers: handler}
}

func (w *serviceAccountReferences) GetHandlers() []handlers.Handler {
	return w.handlers
}

func (*serviceAccountReferences) GetPath() string {
	return "/serviceaccounts/references/validating"
}
