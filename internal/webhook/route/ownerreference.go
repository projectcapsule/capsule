// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package route

import "github.com/projectcapsule/capsule/pkg/runtime/handlers"

type ownerreference struct {
	handlers []handlers.Handler
}

func OwnerReference(handlers ...handlers.Handler) handlers.Webhook {
	return &ownerreference{handlers: handlers}
}

func (w *ownerreference) GetHandlers() []handlers.Handler {
	return w.handlers
}

func (w *ownerreference) GetPath() string {
	return "/namespace-owner-reference"
}
