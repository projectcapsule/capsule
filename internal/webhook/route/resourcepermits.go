// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package route

import "github.com/projectcapsule/capsule/pkg/runtime/handlers"

func ResourcePermitValidation(handler ...handlers.Handler) handlers.Webhook {
	return &resourcePermitValidation{handlers: handler}
}

type resourcePermitValidation struct {
	handlers []handlers.Handler
}

func (v *resourcePermitValidation) GetHandlers() []handlers.Handler {
	return v.handlers
}

func (v *resourcePermitValidation) GetPath() string {
	return "/resourcepermits/validating"
}

func ResourcePermitMutation(handler ...handlers.Handler) handlers.Webhook {
	return &resourcePermitMutation{handlers: handler}
}

type resourcePermitMutation struct {
	handlers []handlers.Handler
}

func (v *resourcePermitMutation) GetHandlers() []handlers.Handler {
	return v.handlers
}

func (v *resourcePermitMutation) GetPath() string {
	return "/resourcepermits/mutating"
}
