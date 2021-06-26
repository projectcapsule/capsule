// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package route

import (
	capsulewebhook "github.com/clastix/capsule/pkg/webhook"
)

// +kubebuilder:webhook:path=/ingresses,mutating=false,sideEffects=None,admissionReviewVersions=v1,failurePolicy=fail,groups=networking.k8s.io;extensions,resources=ingresses,verbs=create;update,versions=v1beta1;v1,name=ingress.capsule.clastix.io

type ingress struct {
	handlers []capsulewebhook.Handler
}

func Ingress(handler ...capsulewebhook.Handler) capsulewebhook.Webhook {
	return &ingress{handlers: handler}
}

func (w *ingress) GetHandlers() []capsulewebhook.Handler {
	return w.handlers
}

func (w *ingress) GetPath() string {
	return "/ingresses"
}
