// Copyright 2020-2023 Project Capsule Authors.
// SPDX-License-Identifier: Apache-2.0

package route

import (
	capsulewebhook "github.com/projectcapsule/capsule/pkg/webhook"
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
