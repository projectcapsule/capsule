// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package route

import (
	capsulewebhook "github.com/clastix/capsule/pkg/webhook"
)

// +kubebuilder:webhook:path=/services,mutating=false,sideEffects=None,admissionReviewVersions=v1,failurePolicy=fail,groups="",resources=services,verbs=create;update,versions=v1,name=services.capsule.clastix.io

type service struct {
	handlers []capsulewebhook.Handler
}

func Service(handler ...capsulewebhook.Handler) capsulewebhook.Webhook {
	return &service{handlers: handler}
}

func (w *service) GetHandlers() []capsulewebhook.Handler {
	return w.handlers
}

func (w *service) GetPath() string {
	return "/services"
}
