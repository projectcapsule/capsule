// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package route

import (
	capsulewebhook "github.com/projectcapsule/capsule/internal/webhook"
)

type poolmutation struct {
	handlers []capsulewebhook.Handler
}

func ResourcePoolMutation(handler ...capsulewebhook.Handler) capsulewebhook.Webhook {
	return &poolmutation{handlers: handler}
}

func (w *poolmutation) GetHandlers() []capsulewebhook.Handler {
	return w.handlers
}

func (w *poolmutation) GetPath() string {
	return "/resourcepool/mutating"
}

type poolclaimmutation struct {
	handlers []capsulewebhook.Handler
}

func ResourcePoolClaimMutation(handler ...capsulewebhook.Handler) capsulewebhook.Webhook {
	return &poolclaimmutation{handlers: handler}
}

func (w *poolclaimmutation) GetHandlers() []capsulewebhook.Handler {
	return w.handlers
}

func (w *poolclaimmutation) GetPath() string {
	return "/resourcepool/claim/mutating"
}

type poolValidation struct {
	handlers []capsulewebhook.Handler
}

func ResourcePoolValidation(handler ...capsulewebhook.Handler) capsulewebhook.Webhook {
	return &poolValidation{handlers: handler}
}

func (w *poolValidation) GetHandlers() []capsulewebhook.Handler {
	return w.handlers
}

func (w *poolValidation) GetPath() string {
	return "/resourcepool/validating"
}

type poolclaimValidation struct {
	handlers []capsulewebhook.Handler
}

func ResourcePoolClaimValidation(handler ...capsulewebhook.Handler) capsulewebhook.Webhook {
	return &poolclaimValidation{handlers: handler}
}

func (w *poolclaimValidation) GetHandlers() []capsulewebhook.Handler {
	return w.handlers
}

func (w *poolclaimValidation) GetPath() string {
	return "/resourcepool/claim/validating"
}
