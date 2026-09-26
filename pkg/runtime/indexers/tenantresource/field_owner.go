// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package tenantresource

import (
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/projectcapsule/capsule/pkg/api/meta"
)

const FieldOwnerIndexerFieldName = "metadata.replicationFieldOwner"

// FieldOwner indexes the immutable parent identity, before replication can run.
// A processed-item index can omit a newly applied target until status catches up.
type FieldOwner struct {
	Obj client.Object
}

func (f FieldOwner) Object() client.Object { return f.Obj }

func (f FieldOwner) Field() string { return FieldOwnerIndexerFieldName }

func (f FieldOwner) Func() client.IndexerFunc {
	return func(obj client.Object) []string {
		return []string{meta.ReplicationFieldOwnerPrefix(obj.GetName(), obj.GetNamespace())}
	}
}
