// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package route

import (
	k8smeta "k8s.io/apimachinery/pkg/api/meta"

	"github.com/projectcapsule/capsule/internal/webhook/rules/status"
	"github.com/projectcapsule/capsule/pkg/runtime/configuration"
	"github.com/projectcapsule/capsule/pkg/runtime/handlers"
)

type rulesValidating struct {
	configuration configuration.Configuration
	mapper        k8smeta.RESTMapper
}

func RulesValidating(mapper k8smeta.RESTMapper, configuration configuration.Configuration) handlers.Webhook {
	return &rulesValidating{
		configuration: configuration,
		mapper:        mapper,
	}
}

func (w *rulesValidating) GetHandlers() []handlers.Handler {
	return []handlers.Handler{
		status.RuleStatusValidationHandler(w.mapper, w.configuration),
	}
}

func (rulesValidating) GetPath() string {
	return "/rulestatus/validating"
}
