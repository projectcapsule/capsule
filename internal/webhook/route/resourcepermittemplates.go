// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package route

import "github.com/projectcapsule/capsule/pkg/runtime/handlers"

func ResourcePermitTemplateValidation(handler ...handlers.Handler) handlers.Webhook {
	return &resourcePermitTemplateValidation{handlers: handler}
}

type resourcePermitTemplateValidation struct {
	handlers []handlers.Handler
}

func (v *resourcePermitTemplateValidation) GetHandlers() []handlers.Handler {
	return v.handlers
}

func (v *resourcePermitTemplateValidation) GetPath() string {
	return "/resourcepermittemplates/validating"
}
