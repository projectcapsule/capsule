// Copyright 2020-2023 Project Capsule Authors.
// SPDX-License-Identifier: Apache-2.0

package route

import (
	capsulewebhook "github.com/projectcapsule/capsule/pkg/webhook"
)

type tntResourceObjsValidation struct {
	handlers []capsulewebhook.Handler
}

func TenantResourceObjectsValidation(handlers ...capsulewebhook.Handler) capsulewebhook.Webhook {
	return &tntResourceObjsValidation{handlers: handlers}
}

func (t tntResourceObjsValidation) GetPath() string {
	return "/tenantresource/objects/validating"
}

func (t tntResourceObjsValidation) GetHandlers() []capsulewebhook.Handler {
	return t.handlers
}

type tntResourcenamespaceMutation struct {
	handlers []capsulewebhook.Handler
}

func TenantResourceNamespacedMutation(handlers ...capsulewebhook.Handler) capsulewebhook.Webhook {
	return &tntResourcenamespaceMutation{handlers: handlers}
}

func (t tntResourcenamespaceMutation) GetPath() string {
	return "/tenantresource/namespaced/mutating"
}

func (t tntResourcenamespaceMutation) GetHandlers() []capsulewebhook.Handler {
	return t.handlers
}

type tntResourceglobalMutation struct {
	handlers []capsulewebhook.Handler
}

func TenantResourceGlobalMutation(handlers ...capsulewebhook.Handler) capsulewebhook.Webhook {
	return &tntResourceglobalMutation{handlers: handlers}
}

func (t tntResourceglobalMutation) GetPath() string {
	return "/tenantresource/global/mutating"
}

func (t tntResourceglobalMutation) GetHandlers() []capsulewebhook.Handler {
	return t.handlers
}
