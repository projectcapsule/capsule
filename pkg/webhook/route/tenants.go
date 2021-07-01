// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package route

import (
	capsulewebhook "github.com/clastix/capsule/pkg/webhook"
)

// +kubebuilder:webhook:path=/tenants,mutating=false,sideEffects=None,admissionReviewVersions=v1,failurePolicy=fail,groups="capsule.clastix.io",resources=tenants,verbs=create;update;delete,versions=v1beta1,name=tenants.capsule.clastix.io

type tenant struct {
	handlers []capsulewebhook.Handler
}

func Tenant(handler ...capsulewebhook.Handler) capsulewebhook.Webhook {
	return &tenant{handlers: handler}
}

func (w *tenant) GetHandlers() []capsulewebhook.Handler {
	return w.handlers
}

func (w *tenant) GetPath() string {
	return "/tenants"
}
