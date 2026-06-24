// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package route

import (
	"github.com/projectcapsule/capsule/internal/webhook/rules/status"
	"github.com/projectcapsule/capsule/pkg/runtime/configuration"
	"github.com/projectcapsule/capsule/pkg/runtime/handlers"
)

type rulesValidating struct {
	configuration configuration.Configuration
}

func RulesValidating(configuration configuration.Configuration) handlers.Webhook {
	return &rulesValidating{
		configuration: configuration,
	}
}

func (w *rulesValidating) GetHandlers() []handlers.Handler {
	return []handlers.Handler{
		status.RuleStatusValidationHandler(w.configuration),
	}
}

func (rulesValidating) GetPath() string {
	return "/rulestatus/validating"
}
