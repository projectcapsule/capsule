// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package route

import "github.com/projectcapsule/capsule/pkg/runtime/handlers"

func GlobalBreakRequestTemplateValidation(handler ...handlers.Handler) handlers.Webhook {
	return &globalBreakRequestTemplateValidation{handlers: handler}
}

type globalBreakRequestTemplateValidation struct {
	handlers []handlers.Handler
}

func (v *globalBreakRequestTemplateValidation) GetHandlers() []handlers.Handler {
	return v.handlers
}

func (v *globalBreakRequestTemplateValidation) GetPath() string {
	return "/globalbreakrequesttemplates/validating"
}
