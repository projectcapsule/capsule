// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package route

import "github.com/projectcapsule/capsule/pkg/runtime/handlers"

func BreakRequestTemplateValidation(handler ...handlers.Handler) handlers.Webhook {
	return &breakRequestTemplateValidation{handlers: handler}
}

type breakRequestTemplateValidation struct {
	handlers []handlers.Handler
}

func (v *breakRequestTemplateValidation) GetHandlers() []handlers.Handler {
	return v.handlers
}

func (v *breakRequestTemplateValidation) GetPath() string {
	return "/breakrequesttemplates/validating"
}
