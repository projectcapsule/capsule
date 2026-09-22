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
	require.Empty(t, CapsuleResourceFieldOwners(nil))
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
			owners := CapsuleResourceFieldOwners(obj)
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
