// Copyright 2020-2023 Project Capsule Authors.
// SPDX-License-Identifier: Apache-2.0

package route

import (
	capsulewebhook "github.com/projectcapsule/capsule/pkg/webhook"
)

type quotamutation struct {
	handlers []capsulewebhook.Handler
}

func QuotaMutation(handler ...capsulewebhook.Handler) capsulewebhook.Webhook {
	return &quotamutation{handlers: handler}
}

func (w *quotamutation) GetHandlers() []capsulewebhook.Handler {
	return w.handlers
}

func (w *quotamutation) GetPath() string {
	return "/globalquota/mutation"
}

type quotaValidation struct {
	handlers []capsulewebhook.Handler
}

func QuotaValidation(handler ...capsulewebhook.Handler) capsulewebhook.Webhook {
	return &quotaValidation{handlers: handler}
}

func (w *quotaValidation) GetHandlers() []capsulewebhook.Handler {
	return w.handlers
}

func (w *quotaValidation) GetPath() string {
	return "/globalquota/validation"
}
