// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package route

import "github.com/projectcapsule/capsule/pkg/runtime/handlers"

func ResourceLeaseTemplateValidation(handler ...handlers.Handler) handlers.Webhook {
	return &resourceLeaseTemplateValidation{handlers: handler}
}

type resourceLeaseTemplateValidation struct {
	handlers []handlers.Handler
}

func (v *resourceLeaseTemplateValidation) GetHandlers() []handlers.Handler {
	return v.handlers
}

func (v *resourceLeaseTemplateValidation) GetPath() string {
	return "/resourceleasetemplates/validating"
}
