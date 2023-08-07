// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package route

import (
	capsulewebhook "github.com/clastix/capsule/pkg/webhook"
)

// +kubebuilder:webhook:path=/managed,mutating=true,sideEffects=None,admissionReviewVersions=v1,failurePolicy=fail,groups="*",resources=*,verbs=create;update,versions="*",name=managed.capsule.clastix.io

type managed struct {
	handlers []capsulewebhook.Handler
}

func Managed(handler ...capsulewebhook.Handler) capsulewebhook.Webhook {
	return &managed{handlers: handler}
}

func (w *managed) GetHandlers() []capsulewebhook.Handler {
	return w.handlers
}

func (w *managed) GetPath() string {
	return "/managed"
}
