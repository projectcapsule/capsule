// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package httproute

import (
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	"github.com/projectcapsule/capsule/pkg/runtime/handlers"
)

// Handler builds a handlers.Handler that wraps all provided
// TypedHandlerWithTenantWithRuleset[*gatewayv1.HTTPRoute] implementations.
func Handler(handler ...handlers.TypedHandlerWithTenantWithRuleset[*gatewayv1.HTTPRoute]) handlers.Handler {
	return &handlers.TypedTenantWithRulesetHandler[*gatewayv1.HTTPRoute]{
		Factory: func() *gatewayv1.HTTPRoute {
			return &gatewayv1.HTTPRoute{}
		},
		Handlers: handler,
	}
}
