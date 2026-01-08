// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

//nolint:dupl
package route

import (
	capsulewebhook "github.com/projectcapsule/capsule/internal/webhook"
)

type tenantValidating struct {
	handlers []capsulewebhook.Handler
}

func TenantValidation(handler ...capsulewebhook.Handler) capsulewebhook.Webhook {
	return &tenantValidating{handlers: handler}
}

func (w *tenantValidating) GetHandlers() []capsulewebhook.Handler {
	return w.handlers
}

func (w *tenantValidating) GetPath() string {
	return "/tenants/validating"
}

type tenantMutating struct {
	handlers []capsulewebhook.Handler
}

func TenantMutation(handler ...capsulewebhook.Handler) capsulewebhook.Webhook {
	return &tenantMutating{handlers: handler}
}

func (w *tenantMutating) GetHandlers() []capsulewebhook.Handler {
	return w.handlers
}

func (w *tenantMutating) GetPath() string {
	return "/tenants/mutating"
}
