// Copyright 2020-2023 Project Capsule Authors.
// SPDX-License-Identifier: Apache-2.0

package route

import (
	capsulewebhook "github.com/projectcapsule/capsule/pkg/webhook"
)

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
