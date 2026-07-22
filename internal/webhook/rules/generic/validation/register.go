// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package validation

import (
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/projectcapsule/capsule/internal/cache"
	"github.com/projectcapsule/capsule/pkg/runtime/handlers"
)

const Path = "/rules/generic/validating"

type genericValidating struct {
	regexCache    *cache.RegexCache
	jsonPathCache *cache.JSONPathCache
}

func Register(regexCache *cache.RegexCache, jsonPathCache *cache.JSONPathCache) handlers.Webhook {
	return &genericValidating{
		regexCache:    regexCache,
		jsonPathCache: jsonPathCache,
	}
}

func (w *genericValidating) GetHandlers() []handlers.Handler {
	return []handlers.Handler{
		genericHandler(
			GenericRules(w.regexCache, w.jsonPathCache),
		),
	}
}

func (genericValidating) GetPath() string {
	return Path
}

func genericHandler(
	handler ...handlers.TypedHandlerWithTenantWithRuleset[*unstructured.Unstructured],
) handlers.Handler {
	return &handlers.TypedTenantWithRulesetHandler[*unstructured.Unstructured]{
		Factory: func() *unstructured.Unstructured {
			return &unstructured.Unstructured{}
		},
		Handlers: handler,
	}
}
