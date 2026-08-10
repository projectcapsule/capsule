// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package route

import "github.com/projectcapsule/capsule/pkg/runtime/handlers"

type globalResourceQuotaCalculation struct {
	handlers []handlers.Handler
}

func GlobalResourceQuotaCalculation(handler ...handlers.Handler) handlers.Webhook {
	return &globalResourceQuotaCalculation{handlers: handler}
}

func (w *globalResourceQuotaCalculation) GetHandlers() []handlers.Handler {
	return w.handlers
}

func (w *globalResourceQuotaCalculation) GetPath() string {
	return "/global-resource-quotas/calculations"
}
