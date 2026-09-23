// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package meta

import (
	"testing"

	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func TestCapsuleResourceFieldOwners(t *testing.T) {
	require.Empty(t, CapsuleResourceFieldOwners(nil, nil))
	for _, tc := range []struct {
		manager string
		owned   bool
	}{
		{ResourceFieldOwner("resourcepermit/request"), true},
		{ResourceControllerFieldOwnerPrefix(), true},
		{"2lclct9cwq6mg/target/tenant-a/0/raw-0/", true},
		{"2lclct9cwq6mg/target/tenant-b/1/generator-0-1/", true},
		{"2lclct9cwq6mg/target/tenant-a/replica/", true},
		{"2lclct9cwq6mg///0/raw-0/", true},
		{"kubectl", false},
		{"external/resource/manager", false},
		{ControllerFieldOwner(), false},
		{"projectcapsule.dev/resourceful/other", false},
		{"invalid-hash/target/tenant/0/raw-0/", false},
		{"zzzzzzzzzzzzzzzz/target/tenant/0/raw-0/", false},
		{"2lclct9cwq6mg/target/tenant/", false},
		{"2lclct9cwq6mg/target/tenant/0/raw-0", false},
	} {
		t.Run(tc.manager, func(t *testing.T) {
			obj := &unstructured.Unstructured{}
			obj.SetManagedFields([]metav1.ManagedFieldsEntry{{Manager: tc.manager}, {Manager: tc.manager}})
			known := map[string]struct{}{}
			if tc.owned {
				known[tc.manager] = struct{}{}
			}
			owners := CapsuleResourceFieldOwners(obj.GetManagedFields(), known)
			_, found := owners[tc.manager]
			require.Equal(t, tc.owned, found)
			if tc.owned {
				require.Len(t, owners, 1)
			} else {
				require.Empty(t, owners)
			}
		})
	}
}

func TestCapsuleResourceFieldOwnersRejectsUnknownReplications(t *testing.T) {
	obj := &unstructured.Unstructured{}
	obj.SetManagedFields([]metav1.ManagedFieldsEntry{
		{Manager: "2lclct9cwq6mg/target/tenant-a/0/raw-0/"},
		{Manager: "2lclct9cwq6mg/target/tenant-b/0/raw-0/"},
	})
	require.Empty(t, CapsuleResourceFieldOwners(obj.GetManagedFields(), nil))
	known := map[string]struct{}{"2lclct9cwq6mg/target/tenant-a/0/raw-0/": {}}
	require.Equal(t, known, CapsuleResourceFieldOwners(obj.GetManagedFields(), known))
}

func TestReplicationFieldOwnerPrefixCompatibility(t *testing.T) {
	require.Equal(t, "3bzzrpaonu5zb", ReplicationFieldOwnerPrefix("replication", ""))
	require.Equal(t, "2elz2dfz41umt", ReplicationFieldOwnerPrefix("replication", "tenant-a"))
	require.Equal(t, "2883zttmw7ldw", ReplicationFieldOwnerPrefix("replication", "tenant-b"))
}
