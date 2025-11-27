// Copyright 2020-2025 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package route

import (
	capsulewebhook "github.com/projectcapsule/capsule/internal/webhook"
)

type miscTenantAssignment struct {
	handlers []capsulewebhook.Handler
}

func MiscTenantAssignment(handlers ...capsulewebhook.Handler) capsulewebhook.Webhook {
	return &miscTenantAssignment{handlers: handlers}
}

func (w miscTenantAssignment) GetPath() string {
	return "/misc/tenant-label"
}

func (w miscTenantAssignment) GetHandlers() []capsulewebhook.Handler {
	return w.handlers
}

type miscManagedValidation struct {
	handlers []capsulewebhook.Handler
}

func MiscManagedValidation(handlers ...capsulewebhook.Handler) capsulewebhook.Webhook {
	return &miscManagedValidation{handlers: handlers}
}

func (t miscManagedValidation) GetPath() string {
	return "/misc/managed"
}

func (t miscManagedValidation) GetHandlers() []capsulewebhook.Handler {
	return t.handlers
}
