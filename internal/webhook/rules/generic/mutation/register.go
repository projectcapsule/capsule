// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package mutation

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

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

func genericHandler(cfg configuration.Configuration, handler ...handlers.TypedHandlerWithTenantWithRuleset[*metav1.PartialObjectMetadata]) handlers.Handler {
	return &handlers.TypedTenantWithRulesetHandler[*metav1.PartialObjectMetadata]{
		Factory:       func() *metav1.PartialObjectMetadata { return &metav1.PartialObjectMetadata{} },
		Handlers:      handler,
		Configuration: cfg,
	}
}
