// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

//nolint:dupl
package route

import "github.com/projectcapsule/capsule/pkg/runtime/handlers"

type tenantValidating struct {
	handlers []handlers.Handler
}

func TenantValidation(handler ...handlers.Handler) handlers.Webhook {
	return &tenantValidating{handlers: handler}
}

func (w *tenantValidating) GetHandlers() []handlers.Handler {
	return w.handlers
}

func (w *tenantValidating) GetPath() string {
	return "/tenants/validating"
}

type tenantMutating struct {
	handlers []handlers.Handler
}

func TenantMutation(handler ...handlers.Handler) handlers.Webhook {
	return &tenantMutating{handlers: handler}
}

func (w *tenantMutating) GetHandlers() []handlers.Handler {
	return w.handlers
}

func (w *tenantMutating) GetPath() string {
	return "/tenants/mutating"
}
