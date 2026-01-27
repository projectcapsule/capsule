// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package route

import "github.com/projectcapsule/capsule/pkg/runtime/handlers"

type miscTenantAssignment struct {
	handlers []handlers.Handler
}

func MiscTenantAssignment(handlers ...handlers.Handler) handlers.Webhook {
	return &miscTenantAssignment{handlers: handlers}
}

func (w miscTenantAssignment) GetPath() string {
	return "/misc/tenant-label"
}

func (w miscTenantAssignment) GetHandlers() []handlers.Handler {
	return w.handlers
}

type miscManagedValidation struct {
	handlers []handlers.Handler
}

func MiscManagedValidation(handlers ...handlers.Handler) handlers.Webhook {
	return &miscManagedValidation{handlers: handlers}
}

func (t miscManagedValidation) GetPath() string {
	return "/misc/managed"
}

func (t miscManagedValidation) GetHandlers() []handlers.Handler {
	return t.handlers
}
