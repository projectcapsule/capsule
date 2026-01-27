// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package route

import "github.com/projectcapsule/capsule/pkg/runtime/handlers"

type tntResourceObjs struct {
	handlers []handlers.Handler
}

func TenantResourceObjects(handlers ...handlers.Handler) handlers.Webhook {
	return &tntResourceObjs{handlers: handlers}
}

func (t tntResourceObjs) GetPath() string {
	return "/tenantresource-objects"
}

func (t tntResourceObjs) GetHandlers() []handlers.Handler {
	return t.handlers
}
