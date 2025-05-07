// Copyright 2020-2023 Project Capsule Authors.
// SPDX-License-Identifier: Apache-2.0

package route

import (
	capsulewebhook "github.com/projectcapsule/capsule/pkg/webhook"
)

type tntResourceObjs struct {
	handlers []capsulewebhook.Handler
}

func TenantResourceObjects(handlers ...capsulewebhook.Handler) capsulewebhook.Webhook {
	return &tntResourceObjs{handlers: handlers}
}

func (t tntResourceObjs) GetPath() string {
	return "/tenantresource-objects"
}

func (t tntResourceObjs) GetHandlers() []capsulewebhook.Handler {
	return t.handlers
}
