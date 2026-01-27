// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package route

import "github.com/projectcapsule/capsule/pkg/runtime/handlers"

type poolmutation struct {
	handlers []handlers.Handler
}

func ResourcePoolMutation(handler ...handlers.Handler) handlers.Webhook {
	return &poolmutation{handlers: handler}
}

func (w *poolmutation) GetHandlers() []handlers.Handler {
	return w.handlers
}

func (w *poolmutation) GetPath() string {
	return "/resourcepool/mutating"
}

type poolclaimmutation struct {
	handlers []handlers.Handler
}

func ResourcePoolClaimMutation(handler ...handlers.Handler) handlers.Webhook {
	return &poolclaimmutation{handlers: handler}
}

func (w *poolclaimmutation) GetHandlers() []handlers.Handler {
	return w.handlers
}

func (w *poolclaimmutation) GetPath() string {
	return "/resourcepool/claim/mutating"
}

type poolValidation struct {
	handlers []handlers.Handler
}

func ResourcePoolValidation(handler ...handlers.Handler) handlers.Webhook {
	return &poolValidation{handlers: handler}
}

func (w *poolValidation) GetHandlers() []handlers.Handler {
	return w.handlers
}

func (w *poolValidation) GetPath() string {
	return "/resourcepool/validating"
}

type poolclaimValidation struct {
	handlers []handlers.Handler
}

func ResourcePoolClaimValidation(handler ...handlers.Handler) handlers.Webhook {
	return &poolclaimValidation{handlers: handler}
}

func (w *poolclaimValidation) GetHandlers() []handlers.Handler {
	return w.handlers
}

func (w *poolclaimValidation) GetPath() string {
	return "/resourcepool/claim/validating"
}
