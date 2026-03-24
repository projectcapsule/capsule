// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package route

import "github.com/projectcapsule/capsule/pkg/runtime/handlers"

type customQuotaValidation struct {
	handlers []handlers.Handler
}

func CustomQuotaValidation(handler ...handlers.Handler) handlers.Webhook {
	return &customQuotaValidation{handlers: handler}
}

func (w *customQuotaValidation) GetHandlers() []handlers.Handler {
	return w.handlers
}

func (w *customQuotaValidation) GetPath() string {
	return "/custom-quotas/namespaced/validating"
}

type globalCustomQuotaValidation struct {
	handlers []handlers.Handler
}

func GlobalCustomQuotaValidation(handler ...handlers.Handler) handlers.Webhook {
	return &globalCustomQuotaValidation{handlers: handler}
}

func (w *globalCustomQuotaValidation) GetHandlers() []handlers.Handler {
	return w.handlers
}

func (w *globalCustomQuotaValidation) GetPath() string {
	return "/custom-quotas/cluster/validating"
}
