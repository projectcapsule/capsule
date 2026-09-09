// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package route

import "github.com/projectcapsule/capsule/pkg/runtime/handlers"

func GlobalResourcePermitTemplateValidation(handler ...handlers.Handler) handlers.Webhook {
	return &globalResourcePermitTemplateValidation{handlers: handler}
}

type globalResourcePermitTemplateValidation struct {
	handlers []handlers.Handler
}

func (v *globalResourcePermitTemplateValidation) GetHandlers() []handlers.Handler {
	return v.handlers
}

func (v *globalResourcePermitTemplateValidation) GetPath() string {
	return "/globalresourcepermittemplates/validating"
}
