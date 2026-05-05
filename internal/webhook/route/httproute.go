// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package route

import "github.com/projectcapsule/capsule/pkg/runtime/handlers"

type httproute struct {
	handlers []handlers.Handler
}

// HTTPRoute returns a Webhook that handles admission requests for
// gateway.networking.k8s.io/v1 HTTPRoute resources.
func HTTPRoute(handler ...handlers.Handler) handlers.Webhook {
	return &httproute{handlers: handler}
}

func (w *httproute) GetHandlers() []handlers.Handler {
	return w.handlers
}

func (w *httproute) GetPath() string {
	return "/httproutes/validating"
}
