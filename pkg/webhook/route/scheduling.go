// Copyright 2020-2023 Project Capsule Authors.
// SPDX-License-Identifier: Apache-2.0

package route

import (
	capsulewebhook "github.com/projectcapsule/capsule/pkg/webhook"
)

// +kubebuilder:webhook:path=/scheduling,mutating=true,sideEffects=None,admissionReviewVersions=v1,failurePolicy=fail,groups="",resources=pods,verbs=create,versions=v1,name=scheduling.projectcapsule.dev

type scheduling struct {
	handlers []capsulewebhook.Handler
}

func Scheduling(handler ...capsulewebhook.Handler) capsulewebhook.Webhook {
	return &scheduling{handlers: handler}
}

func (w *scheduling) GetHandlers() []capsulewebhook.Handler {
	return w.handlers
}

func (w *scheduling) GetPath() string {
	return "/scheduling"
}
