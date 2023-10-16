// Copyright 2020-2023 Project Capsule Authors.
// SPDX-License-Identifier: Apache-2.0

package route

import (
	capsulewebhook "github.com/projectcapsule/capsule/pkg/webhook"
)

// +kubebuilder:webhook:path=/namespaces,mutating=false,sideEffects=None,admissionReviewVersions=v1,failurePolicy=fail,groups="",resources=namespaces,verbs=create;update;delete,versions=v1,name=namespaces.capsule.clastix.io

type namespace struct {
	handlers []capsulewebhook.Handler
}

func Namespace(handler ...capsulewebhook.Handler) capsulewebhook.Webhook {
	return &namespace{handlers: handler}
}

func (w *namespace) GetHandlers() []capsulewebhook.Handler {
	return w.handlers
}

func (w *namespace) GetPath() string {
	return "/namespaces"
}
