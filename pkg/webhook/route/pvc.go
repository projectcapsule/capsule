// Copyright 2020-2023 Project Capsule Authors.
// SPDX-License-Identifier: Apache-2.0

package route

import (
	capsulewebhook "github.com/projectcapsule/capsule/pkg/webhook"
)

// +kubebuilder:webhook:path=/persistentvolumeclaims,mutating=false,sideEffects=None,admissionReviewVersions=v1,failurePolicy=fail,groups="",resources=persistentvolumeclaims,verbs=create,versions=v1,name=pvc.capsule.clastix.io

type pvc struct {
	handlers []capsulewebhook.Handler
}

func PVC(handler ...capsulewebhook.Handler) capsulewebhook.Webhook {
	return &pvc{handlers: handler}
}

func (w *pvc) GetHandlers() []capsulewebhook.Handler {
	return w.handlers
}

func (w *pvc) GetPath() string {
	return "/persistentvolumeclaims"
}
