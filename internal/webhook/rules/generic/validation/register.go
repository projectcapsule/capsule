// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package validation

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/projectcapsule/capsule/internal/cache"
	"github.com/projectcapsule/capsule/pkg/runtime/configuration"
	"github.com/projectcapsule/capsule/pkg/runtime/handlers"
)

const Path = "/rules/generic/validating"

type genericValidating struct {
	regexCache    *cache.RegexCache
	configuration configuration.Configuration
}

func Register(regexCache *cache.RegexCache, cfg configuration.Configuration) handlers.Webhook {
	return &genericValidating{
		regexCache:    regexCache,
		configuration: cfg,
	}
}

func (w *genericValidating) GetHandlers() []handlers.Handler {
	return []handlers.Handler{
		genericHandler(w.configuration,
			GenericRules(w.regexCache),
		),
	}
}

func (genericValidating) GetPath() string {
	return Path
}

func genericHandler(cfg configuration.Configuration,
	handler ...handlers.TypedHandlerWithTenantWithRuleset[*metav1.PartialObjectMetadata],
) handlers.Handler {
	return &handlers.TypedTenantWithRulesetHandler[*metav1.PartialObjectMetadata]{
		Factory: func() *metav1.PartialObjectMetadata {
			return &metav1.PartialObjectMetadata{}
		},
		Handlers:      handler,
		Configuration: cfg,
	}
}
