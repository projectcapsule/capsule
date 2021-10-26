// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package route

import (
	capsulewebhook "github.com/clastix/capsule/pkg/webhook"
)

// +kubebuilder:webhook:path=/nodes,mutating=false,sideEffects=None,admissionReviewVersions=v1,failurePolicy=fail,groups="",resources=nodes,verbs=update,versions=v1,name=nodes.capsule.clastix.io

type node struct {
	handlers []capsulewebhook.Handler
}

func Node(handler ...capsulewebhook.Handler) capsulewebhook.Webhook {
	return &node{handlers: handler}
}

func (n *node) GetHandlers() []capsulewebhook.Handler {
	return n.handlers
}

func (n *node) GetPath() string {
	return "/nodes"
}
