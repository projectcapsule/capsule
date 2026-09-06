// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package route

import "github.com/projectcapsule/capsule/pkg/runtime/handlers"

func ResourceLeaseValidation(handler ...handlers.Handler) handlers.Webhook {
	return &resourceLeaseValidation{handlers: handler}
}

type resourceLeaseValidation struct {
	handlers []handlers.Handler
}

func (v *resourceLeaseValidation) GetHandlers() []handlers.Handler {
	return v.handlers
}

func (v *resourceLeaseValidation) GetPath() string {
	return "/resourceleases/validating"
}

func ResourceLeaseMutation(handler ...handlers.Handler) handlers.Webhook {
	return &resourceLeaseMutation{handlers: handler}
}

type resourceLeaseMutation struct {
	handlers []handlers.Handler
}

func (v *resourceLeaseMutation) GetHandlers() []handlers.Handler {
	return v.handlers
}

func (v *resourceLeaseMutation) GetPath() string {
	return "/resourceleases/mutating"
}
