// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package serviceaccount_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	indexes "github.com/projectcapsule/capsule/pkg/runtime/indexers/serviceaccount"
)

func TestResourcePermitFieldOwner(t *testing.T) {
	index := indexes.ResourcePermitFieldOwner{}
	a := &capsulev1beta2.ResourcePermit{Name: "same", Namespace: "tenant-a", UID: "a"}
	b := &capsulev1beta2.ResourcePermit{Name: "same", Namespace: "tenant-b", UID: "b"}
	require.Nil(t, index.Func()(&capsulev1beta2.ResourcePermit{}))
	require.Equal(t, []string{meta.ResourcePermitFieldOwner(a)}, index.Func()(a))
	scheme := runtime.NewScheme()
	require.NoError(t, capsulev1beta2.AddToScheme(scheme))
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(a, b).WithIndex(index.Object(), index.Field(), index.Func()).Build()
	lookup := func(key string, want int) {
		t.Helper()
		list := &capsulev1beta2.ResourcePermitList{}
		require.NoError(t, c.List(t.Context(), list, client.MatchingFields{index.Field(): key}))
		require.Len(t, list.Items, want)
		if want == 1 {
			require.Equal(t, a.Namespace, list.Items[0].Namespace)
		}
	}
	key := meta.ResourcePermitFieldOwner(a)
	lookup(key, 1)
	require.NoError(t, c.Get(t.Context(), client.ObjectKeyFromObject(a), a))
	a.Status.Request = &capsulev1beta2.ResourcePermitStatusRequest{}
	require.NoError(t, c.Update(t.Context(), a))
	lookup(key, 1)
	require.NoError(t, c.Delete(t.Context(), a))
	lookup(key, 0)
	a.UID = "replacement"
	a.ResourceVersion = ""
	require.NoError(t, c.Create(t.Context(), a))
	lookup(key, 0)
	lookup(meta.ResourcePermitFieldOwner(a), 1)
}
