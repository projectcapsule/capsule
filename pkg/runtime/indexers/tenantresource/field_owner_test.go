// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package tenantresource

import (
	"testing"

	"github.com/stretchr/testify/require"
	"sigs.k8s.io/controller-runtime/pkg/client"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/meta"
)

func TestFieldOwnerIndex(t *testing.T) {
	objects := []client.Object{
		&capsulev1beta2.GlobalTenantResource{Name: "replication"},
		&capsulev1beta2.TenantResource{Name: "replication", Namespace: "tenant-a"},
		&capsulev1beta2.TenantResource{Name: "replication", Namespace: "tenant-b"},
	}
	keys := map[string]struct{}{}
	for _, obj := range objects {
		index := FieldOwner{Obj: obj}
		require.Same(t, obj, index.Object())
		require.Equal(t, FieldOwnerIndexerFieldName, index.Field())
		key := meta.ReplicationFieldOwnerPrefix(obj.GetName(), obj.GetNamespace())
		require.Equal(t, []string{key}, index.Func()(obj), "parent must be discoverable before any processed-item status exists")
		keys[key] = struct{}{}
	}
	require.Len(t, keys, len(objects), "tenant namespaces and global scope need distinct identities")
}
