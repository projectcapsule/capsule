// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package route

import (
	"github.com/projectcapsule/capsule/internal/webhook/generic"
	"github.com/projectcapsule/capsule/pkg/runtime/configuration"
	"github.com/projectcapsule/capsule/pkg/runtime/handlers"
)

type replicasResourcesHandler struct {
	handlers []handlers.Handler
}

func GenericReplicasHandler() handlers.Webhook {
	return &replicasResourcesHandler{}
}

func (w *replicasResourcesHandler) GetHandlers() []handlers.Handler {
	return []handlers.Handler{
		generic.ReplicaHandler(),
	}
}

func (w *replicasResourcesHandler) GetPath() string {
	return "/generic/replicas"
}

type genericCustomResourcesHandler struct {
	handlers []handlers.Handler
}

func GenericCustomResources(handlers ...handlers.Handler) handlers.Webhook {
	return &genericCustomResourcesHandler{handlers: handlers}
}

func (w *genericCustomResourcesHandler) GetHandlers() []handlers.Handler {
	return w.handlers
}

func (w *genericCustomResourcesHandler) GetPath() string {
	return "/generic/customresources"
}

type genericTenantAssignment struct {
	handlers []handlers.Handler
}

func GenericTenantAssignment(handlers ...handlers.Handler) handlers.Webhook {
	return &genericTenantAssignment{handlers: handlers}
}

func (w genericTenantAssignment) GetPath() string {
	return "/generic/tenant-label"
}

func (w genericTenantAssignment) GetHandlers() []handlers.Handler {
	return w.handlers
}

type miscManagedValidation struct {
	configuration configuration.Configuration
}

func GenericManagedHandler(cfg configuration.Configuration) handlers.Webhook {
	return &miscManagedValidation{configuration: cfg}
}

func (t miscManagedValidation) GetPath() string {
	return "/generic/managed"
}

func (t miscManagedValidation) GetHandlers() []handlers.Handler {
	return []handlers.Handler{
		handlers.InCapsuleGroups(t.configuration, generic.ManagedValidatingHandler()),
	}
}
