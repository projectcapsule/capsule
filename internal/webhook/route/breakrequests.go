// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package route

import "github.com/projectcapsule/capsule/pkg/runtime/handlers"

func BreakRequestValidation(handler ...handlers.Handler) handlers.Webhook {
	return &breakRequestValidation{handlers: handler}
}

type breakRequestValidation struct {
	handlers []handlers.Handler
}

func (v *breakRequestValidation) GetHandlers() []handlers.Handler {
	return v.handlers
}

func (v *breakRequestValidation) GetPath() string {
	return "/breakrequests/validating"
}

func BreakRequestMutation(handler ...handlers.Handler) handlers.Webhook {
	return &breakRequestMutation{handlers: handler}
}

type breakRequestMutation struct {
	handlers []handlers.Handler
}

func (v *breakRequestMutation) GetHandlers() []handlers.Handler {
	return v.handlers
}

func (v *breakRequestMutation) GetPath() string {
	return "/breakrequests/mutating"
}
