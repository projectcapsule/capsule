// Copyright 2020-2023 Project Capsule Authors.
// SPDX-License-Identifier: Apache-2.0

package route

import (
	capsulewebhook "github.com/projectcapsule/capsule/pkg/webhook"
)

type cordoning struct {
	handlers []capsulewebhook.Handler
}

func Cordoning(handlers ...capsulewebhook.Handler) capsulewebhook.Webhook {
	return &cordoning{handlers: handlers}
}

func (w cordoning) GetPath() string {
	return "/cordoning"
}

func (w cordoning) GetHandlers() []capsulewebhook.Handler {
	return w.handlers
}
