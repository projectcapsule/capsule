// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package rbac_test

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/projectcapsule/capsule/pkg/api/rbac"
)

func TestOwnerListSpec_FindOwner(t *testing.T) {
	bla := rbac.OwnerSpec{
		Kind: rbac.UserOwner,
		Name: "bla",
		ProxyOperations: []rbac.ProxySettings{
			{
				Kind:       rbac.IngressClassesProxy,
				Operations: []rbac.ProxyOperation{"Delete"},
			},
		},
	}
	bar := rbac.OwnerSpec{
		Kind: rbac.GroupOwner,
		Name: "bar",
		ProxyOperations: []rbac.ProxySettings{
			{
				Kind:       rbac.StorageClassesProxy,
				Operations: []rbac.ProxyOperation{"Delete"},
			},
		},
	}
	baz := rbac.OwnerSpec{
		Kind: rbac.UserOwner,
		Name: "baz",
		ProxyOperations: []rbac.ProxySettings{
			{
				Kind:       rbac.StorageClassesProxy,
				Operations: []rbac.ProxyOperation{"Update"},
			},
		},
	}
	fim := rbac.OwnerSpec{
		Kind: rbac.ServiceAccountOwner,
		Name: "fim",
		ProxyOperations: []rbac.ProxySettings{
			{
				Kind:       rbac.NodesProxy,
				Operations: []rbac.ProxyOperation{"List"},
			},
		},
	}
	bom := rbac.OwnerSpec{
		Kind: rbac.GroupOwner,
		Name: "bom",
		ProxyOperations: []rbac.ProxySettings{
			{
				Kind:       rbac.StorageClassesProxy,
				Operations: []rbac.ProxyOperation{"Delete"},
			},
			{
				Kind:       rbac.NodesProxy,
				Operations: []rbac.ProxyOperation{"Delete"},
			},
		},
	}
	qip := rbac.OwnerSpec{
		Kind: rbac.ServiceAccountOwner,
		Name: "qip",
		ProxyOperations: []rbac.ProxySettings{
			{
				Kind:       rbac.StorageClassesProxy,
				Operations: []rbac.ProxyOperation{"List", "Delete"},
			},
		},
	}
	owners := rbac.OwnerListSpec{bom, qip, bla, bar, baz, fim}

	assert.Equal(t, owners.FindOwner("bom", rbac.GroupOwner), bom)
	assert.Equal(t, owners.FindOwner("qip", rbac.ServiceAccountOwner), qip)
	assert.Equal(t, owners.FindOwner("bla", rbac.UserOwner), bla)
	assert.Equal(t, owners.FindOwner("bar", rbac.GroupOwner), bar)
	assert.Equal(t, owners.FindOwner("baz", rbac.UserOwner), baz)
	assert.Equal(t, owners.FindOwner("fim", rbac.ServiceAccountOwner), fim)
	assert.Equal(t, owners.FindOwner("notfound", rbac.ServiceAccountOwner), rbac.OwnerSpec{})
}

// Regression: the search predicate Kind >= kind && Name >= name is not
// monotonic under the (Kind, Name) sort order. With a Group owner named "bob"
// and a User owner named "alice" it yields [true, false], sort.Search returns
// len(owners), and the existing Group owner was missed (zero value returned).
func TestOwnerListSpec_FindOwner_KindNameOrdering(t *testing.T) {
	bob := rbac.OwnerSpec{
		Kind: rbac.GroupOwner,
		Name: "bob",
	}
	alice := rbac.OwnerSpec{
		Kind: rbac.UserOwner,
		Name: "alice",
	}
	owners := rbac.OwnerListSpec{bob, alice}

	assert.Equal(t, bob, owners.FindOwner("bob", rbac.GroupOwner))
	assert.Equal(t, alice, owners.FindOwner("alice", rbac.UserOwner))
	assert.Equal(t, rbac.OwnerSpec{}, owners.FindOwner("notfound", rbac.GroupOwner))
}

// Regression: FindOwner used to sort the receiver in place, mutating slices
// shared with the manager cache and racing concurrent readers.
func TestOwnerListSpec_FindOwner_InputNotMutated(t *testing.T) {
	owners := rbac.OwnerListSpec{
		{Kind: rbac.UserOwner, Name: "alice"},
		{Kind: rbac.GroupOwner, Name: "bob"},
	}
	want := rbac.OwnerListSpec{
		{Kind: rbac.UserOwner, Name: "alice"},
		{Kind: rbac.GroupOwner, Name: "bob"},
	}

	owners.FindOwner("alice", rbac.UserOwner)

	assert.Equal(t, want, owners)
}

func BenchmarkOwnerListSpec_FindOwner(b *testing.B) {
	kinds := []rbac.OwnerKind{rbac.GroupOwner, rbac.ServiceAccountOwner, rbac.UserOwner}

	for _, size := range []int{1, 10, 100} {
		single := make(rbac.OwnerListSpec, size)
		mixed := make(rbac.OwnerListSpec, size)

		for i := range size {
			single[i] = rbac.OwnerSpec{Kind: rbac.UserOwner, Name: fmt.Sprintf("owner-%03d", i)}
			mixed[i] = rbac.OwnerSpec{Kind: kinds[i%len(kinds)], Name: fmt.Sprintf("owner-%03d", i)}
		}

		cases := []struct {
			name      string
			owners    rbac.OwnerListSpec
			queryName string
			queryKind rbac.OwnerKind
			want      rbac.OwnerSpec
		}{
			{"single-kind/hit", single, single[size-1].Name, rbac.UserOwner, single[size-1]},
			{"single-kind/miss", single, "zzz-missing", rbac.UserOwner, rbac.OwnerSpec{}},
			{"mixed-kind/hit", mixed, mixed[size-1].Name, mixed[size-1].Kind, mixed[size-1]},
			{"mixed-kind/miss", mixed, "zzz-missing", rbac.UserOwner, rbac.OwnerSpec{}},
		}

		for _, tc := range cases {
			b.Run(fmt.Sprintf("%s/n=%d", tc.name, size), func(b *testing.B) {
				b.ReportAllocs()

				for b.Loop() {
					got := tc.owners.FindOwner(tc.queryName, tc.queryKind)
					if got.Kind != tc.want.Kind || got.Name != tc.want.Name {
						b.Fatalf("FindOwner(%q, %q) = {%q, %q}, want {%q, %q}",
							tc.queryName, tc.queryKind, got.Kind, got.Name, tc.want.Kind, tc.want.Name)
					}
				}
			})
		}
	}

	b.Run("mixed-kind/hit/parallel/n=100", func(b *testing.B) {
		owners := make(rbac.OwnerListSpec, 100)
		for i := range owners {
			owners[i] = rbac.OwnerSpec{Kind: kinds[i%len(kinds)], Name: fmt.Sprintf("owner-%03d", i)}
		}

		want := owners[len(owners)-1]

		b.ReportAllocs()
		b.RunParallel(func(pb *testing.PB) {
			for pb.Next() {
				got := owners.FindOwner(want.Name, want.Kind)
				if got.Kind != want.Kind || got.Name != want.Name {
					b.Fatalf("FindOwner(%q, %q) = {%q, %q}, want {%q, %q}",
						want.Name, want.Kind, got.Kind, got.Name, want.Kind, want.Name)
				}
			}
		})
	})
}
