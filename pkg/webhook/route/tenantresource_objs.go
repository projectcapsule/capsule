// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package route

import (
	capsulewebhook "github.com/clastix/capsule/pkg/webhook"
)

// +kubebuilder:webhook:path=/tenantresource-objects,mutating=false,sideEffects=None,admissionReviewVersions=v1,failurePolicy=fail,groups="*",resources="*",verbs=update;delete,versions="*",name=resource-objects.tenant.capsule.clastix.io

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
