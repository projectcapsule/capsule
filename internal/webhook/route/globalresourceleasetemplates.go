// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package route

import "github.com/projectcapsule/capsule/pkg/runtime/handlers"

func GlobalResourceLeaseTemplateValidation(handler ...handlers.Handler) handlers.Webhook {
	return &globalResourceLeaseTemplateValidation{handlers: handler}
}

type globalResourceLeaseTemplateValidation struct {
	handlers []handlers.Handler
}

func (v *globalResourceLeaseTemplateValidation) GetHandlers() []handlers.Handler {
	return v.handlers
}

func (v *globalResourceLeaseTemplateValidation) GetPath() string {
	return "/globalresourceleasetemplates/validating"
}
