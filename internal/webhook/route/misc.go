// Copyright 2020-2025 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package route

import (
	capsulewebhook "github.com/projectcapsule/capsule/internal/webhook"
)

type miscTenantAssignment struct {
	handlers []capsulewebhook.Handler
}

func TenantAssignment(handlers ...capsulewebhook.Handler) capsulewebhook.Webhook {
	return &miscTenantAssignment{handlers: handlers}
}

func (w miscTenantAssignment) GetPath() string {
	return "/misc/tenant_assignment"
}

func (w miscTenantAssignment) GetHandlers() []capsulewebhook.Handler {
	return w.handlers
}
