// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package route

import (
	capsulewebhook "github.com/clastix/capsule/pkg/webhook"
)

// +kubebuilder:webhook:path=/networkpolicies,mutating=false,sideEffects=None,admissionReviewVersions=v1,failurePolicy=fail,groups="networking.k8s.io",resources=networkpolicies,verbs=update;delete,versions=v1,name=networkpolicies.capsule.clastix.io

type networkPolicy struct {
	handlers []capsulewebhook.Handler
}

func NetworkPolicy(handler ...capsulewebhook.Handler) capsulewebhook.Webhook {
	return &networkPolicy{handlers: handler}
}

func (w *networkPolicy) GetHandlers() []capsulewebhook.Handler {
	return w.handlers
}

func (w *networkPolicy) GetPath() string {
	return "/networkpolicies"
}
