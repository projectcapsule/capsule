// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package mutation

import (
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/projectcapsule/capsule/pkg/runtime/configuration"
	"github.com/projectcapsule/capsule/pkg/runtime/handlers"
)

const Path = "/rules/generic/mutating"

type genericMutating struct{ configuration configuration.Configuration }

func Register(cfg configuration.Configuration) handlers.Webhook {
	return &genericMutating{configuration: cfg}
}

func (g genericMutating) GetHandlers() []handlers.Handler {
	return []handlers.Handler{genericHandler(g.configuration, MetadataRules())}
}

func (genericMutating) GetPath() string { return Path }

func genericHandler(cfg configuration.Configuration, handler ...handlers.TypedHandlerWithTenantWithRuleset[*unstructured.Unstructured]) handlers.Handler {
	return &handlers.TypedTenantWithRulesetHandler[*unstructured.Unstructured]{
		Factory:       func() *unstructured.Unstructured { return &unstructured.Unstructured{} },
		Handlers:      handler,
		Configuration: cfg,
	}
}
