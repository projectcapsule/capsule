// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package v1beta1

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestOwnerListSpec_FindOwner(t *testing.T) {
	bla := OwnerSpec{
		Kind: UserOwner,
		Name: "bla",
		ProxyOperations: []ProxySettings{
			{
				Kind:       IngressClassesProxy,
				Operations: []ProxyOperation{"Delete"},
			},
		},
	}
	bar := OwnerSpec{
		Kind: GroupOwner,
		Name: "bar",
		ProxyOperations: []ProxySettings{
			{
				Kind:       StorageClassesProxy,
				Operations: []ProxyOperation{"Delete"},
			},
		},
	}
	baz := OwnerSpec{
		Kind: UserOwner,
		Name: "baz",
		ProxyOperations: []ProxySettings{
			{
				Kind:       StorageClassesProxy,
				Operations: []ProxyOperation{"Update"},
			},
		},
	}
	fim := OwnerSpec{
		Kind: ServiceAccountOwner,
		Name: "fim",
		ProxyOperations: []ProxySettings{
			{
				Kind:       NodesProxy,
				Operations: []ProxyOperation{"List"},
			},
		},
	}
	bom := OwnerSpec{
		Kind: GroupOwner,
		Name: "bom",
		ProxyOperations: []ProxySettings{
			{
				Kind:       StorageClassesProxy,
				Operations: []ProxyOperation{"Delete"},
			},
			{
				Kind:       NodesProxy,
				Operations: []ProxyOperation{"Delete"},
			},
		},
	}
	qip := OwnerSpec{
		Kind: ServiceAccountOwner,
		Name: "qip",
		ProxyOperations: []ProxySettings{
			{
				Kind:       StorageClassesProxy,
				Operations: []ProxyOperation{"List", "Delete"},
			},
		},
	}
	owners := OwnerListSpec{bom, qip, bla, bar, baz, fim}

	assert.Equal(t, owners.FindOwner("bom", GroupOwner), bom)
	assert.Equal(t, owners.FindOwner("qip", ServiceAccountOwner), qip)
	assert.Equal(t, owners.FindOwner("bla", UserOwner), bla)
	assert.Equal(t, owners.FindOwner("bar", GroupOwner), bar)
	assert.Equal(t, owners.FindOwner("baz", UserOwner), baz)
	assert.Equal(t, owners.FindOwner("fim", ServiceAccountOwner), fim)
	assert.Equal(t, owners.FindOwner("notfound", ServiceAccountOwner), OwnerSpec{})

	// Regression: the search predicate Kind >= kind && Name >= name is not
	// monotonic under the (Kind, Name) sort order. Here it yields
	// [true, false], sort.Search returns len(owners), and the existing Group
	// owner was missed (zero value returned).
	bob := OwnerSpec{Kind: GroupOwner, Name: "bob"}
	nonMonotonic := OwnerListSpec{
		bob,
		{Kind: UserOwner, Name: "alice"},
	}
	assert.Equal(t, nonMonotonic.FindOwner("bob", GroupOwner), bob)
}

// Regression: FindOwner used to sort the receiver in place, mutating slices
// shared with the manager cache and racing concurrent readers.
func TestOwnerListSpec_FindOwner_InputNotMutated(t *testing.T) {
	owners := OwnerListSpec{
		{Kind: UserOwner, Name: "alice"},
		{Kind: GroupOwner, Name: "bob"},
	}
	want := OwnerListSpec{
		{Kind: UserOwner, Name: "alice"},
		{Kind: GroupOwner, Name: "bob"},
	}

	owners.FindOwner("alice", UserOwner)

	assert.Equal(t, want, owners)
}

func BenchmarkOwnerListSpec_FindOwner(b *testing.B) {
	kinds := []OwnerKind{GroupOwner, ServiceAccountOwner, UserOwner}

	for _, size := range []int{1, 10, 100} {
		single := make(OwnerListSpec, size)
		mixed := make(OwnerListSpec, size)

		for i := range size {
			single[i] = OwnerSpec{Kind: UserOwner, Name: fmt.Sprintf("owner-%03d", i)}
			mixed[i] = OwnerSpec{Kind: kinds[i%len(kinds)], Name: fmt.Sprintf("owner-%03d", i)}
		}

		cases := []struct {
			name      string
			owners    OwnerListSpec
			queryName string
			queryKind OwnerKind
			want      OwnerSpec
		}{
			{"single-kind/hit", single, single[size-1].Name, UserOwner, single[size-1]},
			{"single-kind/miss", single, "zzz-missing", UserOwner, OwnerSpec{}},
			{"mixed-kind/hit", mixed, mixed[size-1].Name, mixed[size-1].Kind, mixed[size-1]},
			{"mixed-kind/miss", mixed, "zzz-missing", UserOwner, OwnerSpec{}},
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
}
