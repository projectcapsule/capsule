// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package route

import (
	capsulewebhook "github.com/clastix/capsule/pkg/webhook"
)

// +kubebuilder:webhook:path=/defaults,mutating=true,sideEffects=None,admissionReviewVersions=v1,failurePolicy=fail,groups="",resources=pods,verbs=create,versions=v1,name=pod.defaults.capsule.clastix.io
// +kubebuilder:webhook:path=/defaults,mutating=true,sideEffects=None,admissionReviewVersions=v1,failurePolicy=fail,groups="",resources=persistentvolumeclaims,verbs=create,versions=v1,name=storage.defaults.capsule.clastix.io
// +kubebuilder:webhook:path=/defaults,mutating=true,sideEffects=None,admissionReviewVersions=v1,failurePolicy=fail,groups=networking.k8s.io,resources=ingresses,verbs=create;update,versions=v1beta1;v1,name=ingress.defaults.capsule.clastix.io

type defaults struct {
	handlers []capsulewebhook.Handler
}

func Defaults(handler ...capsulewebhook.Handler) capsulewebhook.Webhook {
	return &defaults{handlers: handler}
}

func (w *defaults) GetHandlers() []capsulewebhook.Handler {
	return w.handlers
}

func (w *defaults) GetPath() string {
	return "/defaults"
}
