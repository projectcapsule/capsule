// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package route

import "github.com/projectcapsule/capsule/pkg/runtime/handlers"

type tenantOwnersValidating struct {
	handlers []handlers.Handler
}

func TenantOwnersValidation(handler ...handlers.Handler) handlers.Webhook {
	return &tenantOwnersValidating{handlers: handler}
}

func (w *tenantOwnersValidating) GetHandlers() []handlers.Handler {
	return w.handlers
}

func (w *tenantOwnersValidating) GetPath() string {
	return "/tenantowners/validating"
}
