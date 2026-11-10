// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package route

import (
	"github.com/projectcapsule/capsule/pkg/runtime/handlers"
	"github.com/projectcapsule/capsule/pkg/runtime/replication"
)

type replicationTriggers struct{ handler handlers.Handler }

func ReplicationTriggers(handler handlers.Handler) handlers.Webhook {
	return &replicationTriggers{handler: handler}
}
func (r *replicationTriggers) GetPath() string                 { return replication.TriggerPath }
func (r *replicationTriggers) GetHandlers() []handlers.Handler { return []handlers.Handler{r.handler} }
