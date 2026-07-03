// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package route

import "github.com/projectcapsule/capsule/pkg/runtime/handlers"

type tenantResourceValidation struct {
	handlers []handlers.Handler
}

// TenantResourceValidation validates TenantResource and GlobalTenantResource
// objects (both carry spec.healthChecks) under a single path.
func TenantResourceValidation(handler ...handlers.Handler) handlers.Webhook {
	return &tenantResourceValidation{handlers: handler}
}

func (w *tenantResourceValidation) GetHandlers() []handlers.Handler {
	return w.handlers
}

func (w *tenantResourceValidation) GetPath() string {
	return "/tenantresources/validating"
}
