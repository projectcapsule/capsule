// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package route

import (
	"net/http"

	"github.com/projectcapsule/capsule/pkg/runtime/handlers"
	"github.com/projectcapsule/capsule/pkg/runtime/replication"
)

type replicationTriggers struct {
	handler  handlers.Handler
	delivery *replication.DeliveryKey
}

func ReplicationTriggers(handler handlers.Handler, delivery *replication.DeliveryKey) handlers.Webhook {
	return &replicationTriggers{handler: handler, delivery: delivery}
}

func (r *replicationTriggers) WrapHandler(next http.Handler) http.Handler {
	return r.delivery.WrapHandler(next)
}
func (r *replicationTriggers) GetPath() string                 { return replication.TriggerPath }
func (r *replicationTriggers) GetHandlers() []handlers.Handler { return []handlers.Handler{r.handler} }
