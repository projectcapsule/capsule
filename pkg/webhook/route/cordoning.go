// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package route

import (
	capsulewebhook "github.com/clastix/capsule/pkg/webhook"
)

// +kubebuilder:webhook:path=/cordoning,mutating=false,sideEffects=None,admissionReviewVersions=v1,failurePolicy=fail,groups="*",resources="*",verbs=create;update;delete,versions="*",name=cordoning.tenant.capsule.clastix.io

type cordoning struct {
	handlers []capsulewebhook.Handler
}

func Cordoning(handlers ...capsulewebhook.Handler) capsulewebhook.Webhook {
	return &cordoning{handlers: handlers}
}

func (w cordoning) GetPath() string {
	return "/cordoning"
}

func (w cordoning) GetHandlers() []capsulewebhook.Handler {
	return w.handlers
}
