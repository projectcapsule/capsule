// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package route

import "github.com/projectcapsule/capsule/pkg/runtime/handlers"

type node struct {
	handlers []handlers.Handler
}

func Node(handler ...handlers.Handler) handlers.Webhook {
	return &node{handlers: handler}
}

func (n *node) GetHandlers() []handlers.Handler {
	return n.handlers
}

func (n *node) GetPath() string {
	return "/nodes"
}
