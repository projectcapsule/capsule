// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package route

import (
	capsulewebhook "github.com/clastix/capsule/pkg/webhook"
)

// +kubebuilder:webhook:path=/pods,mutating=false,sideEffects=None,admissionReviewVersions=v1,failurePolicy=fail,groups="",resources=pods,verbs=create,versions=v1,name=pods.capsule.clastix.io

type pod struct {
	handlers []capsulewebhook.Handler
}

func Pod(handler ...capsulewebhook.Handler) capsulewebhook.Webhook {
	return &pod{handlers: handler}
}

func (w *pod) GetHandlers() []capsulewebhook.Handler {
	return w.handlers
}

func (w *pod) GetPath() string {
	return "/pods"
}
