// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package route

import (
	capsulewebhook "github.com/projectcapsule/capsule/internal/webhook"
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
