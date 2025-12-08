// Copyright 2020-2025 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package api_test

import (
	"math/rand"
	"reflect"
	"sort"
	"testing"
	"time"

	"github.com/projectcapsule/capsule/pkg/api"
)

func slowIsOwner(o api.OwnerStatusListSpec, name string, groups []string) bool {
	for _, owner := range o {
		switch owner.Kind {
		case api.UserOwner, api.ServiceAccountOwner:
			if name == owner.Name {
				return true
			}
		case api.GroupOwner:
			for _, group := range groups {
				if group == owner.Name {
					return true
				}
			}
		}
	}
	return false
}

// linearFind is the obvious, slow, but correct reference implementation.
func linearFind(o api.OwnerStatusListSpec, name string, kind api.OwnerKind) (api.CoreOwnerSpec, bool) {
	for _, x := range o {
		if x.Kind == kind && x.Name == name {
			return x, true
		}
	}
	return api.CoreOwnerSpec{}, false
}

// randomName generates a simple lowercase name of length n.
func randomName(rnd *rand.Rand, n int) string {
	const letters = "abcdefghijklmnopqrstuvwxyz"
	b := make([]byte, n)
	for i := range b {
		b[i] = letters[rnd.Intn(len(letters))]
	}
	return string(b)
}

func TestUpsert_AddsNewOwnerToEmptyList(t *testing.T) {
	var list api.OwnerStatusListSpec

	list.Upsert(api.CoreOwnerSpec{
		UserSpec: api.UserSpec{
			Kind: api.UserOwner,
			Name: "alice",
		},
		ClusterRoles: []string{"admin"},
	})

	if len(list) != 1 {
		t.Fatalf("expected 1 owner, got %d", len(list))
	}
	got := list[0]
	if got.Kind != api.UserOwner || got.Name != "alice" {
		t.Fatalf("unexpected owner: %+v", got)
	}
	if !reflect.DeepEqual(got.ClusterRoles, []string{"admin"}) {
		t.Fatalf("unexpected roles: %#v", got.ClusterRoles)
	}
}

func TestUpsert_MergesClusterRolesForExistingOwner(t *testing.T) {
	list := api.OwnerStatusListSpec{
		{
			UserSpec: api.UserSpec{
				Kind: api.UserOwner,
				Name: "alice",
			},
			ClusterRoles: []string{"admin", "capsule-namespace-deleter"},
		},
	}

	list.Upsert(api.CoreOwnerSpec{
		UserSpec: api.UserSpec{
			Kind: api.UserOwner,
			Name: "alice",
		},
		ClusterRoles: []string{"extra-sad"},
	})

	if len(list) != 1 {
		t.Fatalf("expected 1 owner, got %d", len(list))
	}
	got := list[0]
	if got.Kind != api.UserOwner || got.Name != "alice" {
		t.Fatalf("unexpected owner: %+v", got)
	}

	// Roles should be union of both sets, order: existing roles first, then new ones
	expected := []string{"admin", "capsule-namespace-deleter", "extra-sad"}
	if !reflect.DeepEqual(got.ClusterRoles, expected) {
		t.Fatalf("expected roles %v, got %v", expected, got.ClusterRoles)
	}
}

func TestUpsert_DeduplicatesClusterRoles(t *testing.T) {
	list := api.OwnerStatusListSpec{
		{
			UserSpec: api.UserSpec{
				Kind: api.UserOwner,
				Name: "alice",
			},
			ClusterRoles: []string{"admin", "viewer"},
		},
	}

	list.Upsert(api.CoreOwnerSpec{
		UserSpec: api.UserSpec{
			Kind: api.UserOwner,
			Name: "alice",
		},
		ClusterRoles: []string{"viewer", "editor"},
	})

	if len(list) != 1 {
		t.Fatalf("expected 1 owner, got %d", len(list))
	}
	got := list[0]

	expected := []string{"admin", "viewer", "editor"}
	if !reflect.DeepEqual(got.ClusterRoles, expected) {
		t.Fatalf("expected roles %v, got %v", expected, got.ClusterRoles)
	}
}

func TestUpsert_KeepsListSortedAndMergesIntoExistingInUnsortedInitialSlice(t *testing.T) {
	// Start with an unsorted slice, as could come from API/server
	list := api.OwnerStatusListSpec{
		{
			UserSpec: api.UserSpec{
				Kind: api.UserOwner,
				Name: "bob",
			},
			ClusterRoles: []string{"bob-role"},
		},
		{
			UserSpec: api.UserSpec{
				Kind: api.UserOwner,
				Name: "alice",
			},
			ClusterRoles: []string{"admin"},
		},
	}

	// Upsert another alice
	list.Upsert(api.CoreOwnerSpec{
		UserSpec: api.UserSpec{
			Kind: api.UserOwner,
			Name: "alice",
		},
		ClusterRoles: []string{"extra"},
	})

	if len(list) != 2 {
		t.Fatalf("expected 2 owners (alice, bob), got %d", len(list))
	}

	// Ensure sorted by Kind.Name: alice before bob
	// (relies on ByKindAndName order)
	sorted := make(api.OwnerStatusListSpec, len(list))
	copy(sorted, list)
	sort.Sort(api.GetByKindAndName(sorted))

	if !reflect.DeepEqual(list, sorted) {
		t.Fatalf("expected list to be sorted by kind+name, got %#v", list)
	}

	// Find alice and check roles
	var alice *api.CoreOwnerSpec
	for i := range list {
		if list[i].Name == "alice" {
			alice = &list[i]
			break
		}
	}
	if alice == nil {
		t.Fatalf("alice not found in list")
	}

	expectedRoles := []string{"admin", "extra"}
	if !reflect.DeepEqual(alice.ClusterRoles, expectedRoles) {
		t.Fatalf("expected alice roles %v, got %v", expectedRoles, alice.ClusterRoles)
	}
}

func TestGetByKindAndNameOrdering(t *testing.T) {
	o := api.OwnerStatusListSpec{
		api.CoreOwnerSpec{UserSpec: api.UserSpec{Name: "b", Kind: api.ServiceAccountOwner}},
		api.CoreOwnerSpec{UserSpec: api.UserSpec{Name: "z", Kind: api.UserOwner}},
		api.CoreOwnerSpec{UserSpec: api.UserSpec{Name: "a", Kind: api.GroupOwner}},
		api.CoreOwnerSpec{UserSpec: api.UserSpec{Name: "a", Kind: api.UserOwner}},
	}

	// Sort using production ordering
	got := append(api.OwnerStatusListSpec(nil), o...)
	sort.Sort(api.GetByKindAndName(got))

	// Manually sorted expectation using the same logic.
	want := append(api.OwnerStatusListSpec(nil), o...)
	sort.Slice(want, func(i, j int) bool {
		if want[i].Kind.String() != want[j].Kind.String() {
			return want[i].Kind.String() < want[j].Kind.String()
		}
		return want[i].Name < want[j].Name
	})

	if len(got) != len(want) {
		t.Fatalf("length mismatch: got %d, want %d", len(got), len(want))
	}
	for i := range got {
		if !reflect.DeepEqual(got[i], want[i]) {
			t.Fatalf("ordering mismatch at %d: got %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestFindOwner_Randomized(t *testing.T) {
	rnd := rand.New(rand.NewSource(42)) // fixed seed for deterministic test runs

	ownerKinds := []api.OwnerKind{
		api.GroupOwner,
		api.UserOwner,
		api.ServiceAccountOwner,
	}

	const (
		numLists          = 200
		maxLength         = 40
		numLookupsPerList = 80
	)

	for listIdx := 0; listIdx < numLists; listIdx++ {
		var list api.OwnerStatusListSpec
		n := rnd.Intn(maxLength)
		for i := 0; i < n; i++ {
			k := ownerKinds[rnd.Intn(len(ownerKinds))]
			list = append(list, api.CoreOwnerSpec{
				UserSpec: api.UserSpec{
					Name: randomName(rnd, 3+rnd.Intn(4)), // length 3–6
					Kind: k,
				},
			})
		}

		for lookupIdx := 0; lookupIdx < numLookupsPerList; lookupIdx++ {
			var qName string
			var qKind api.OwnerKind

			if len(list) > 0 && rnd.Float64() < 0.6 {
				// 60% of lookups: pick a real element, must be found
				pick := list[rnd.Intn(len(list))]
				qName = pick.Name
				qKind = pick.Kind
			} else {
				// 40%: random query, may or may not exist
				qName = randomName(rnd, 3+rnd.Intn(4))
				qKind = ownerKinds[rnd.Intn(len(ownerKinds))]
			}

			listCopy := append(api.OwnerStatusListSpec(nil), list...)
			gotOwner, gotFound := listCopy.FindOwner(qName, qKind)
			wantOwner, wantFound := linearFind(list, qName, qKind)

			if gotFound != wantFound {
				t.Fatalf("list=%d lookup=%d: found mismatch for (%q,%v): got=%v, want=%v",
					listIdx, lookupIdx, qName, qKind, gotFound, wantFound)
			}
			if gotFound && !reflect.DeepEqual(gotOwner, wantOwner) {
				t.Fatalf("list=%d lookup=%d: owner mismatch for (%q,%v):\n got= %+v\nwant= %+v",
					listIdx, lookupIdx, qName, qKind, gotOwner, wantOwner)
			}
		}
	}
}

func TestIsOwner_RandomizedMatchesSlowImplementation(t *testing.T) {
	rnd := rand.New(rand.NewSource(time.Now().UnixNano()))

	ownerKinds := []api.OwnerKind{
		api.UserOwner,
		api.GroupOwner,
		api.ServiceAccountOwner,
	}

	const (
		numLists          = 200
		maxOwnersPerList  = 30
		numLookupsPerList = 80
		maxGroupsPerUser  = 10
	)

	for listIdx := 0; listIdx < numLists; listIdx++ {
		// Generate a random owner list (possibly with duplicates).
		var owners api.OwnerStatusListSpec
		nOwners := rnd.Intn(maxOwnersPerList)
		for i := 0; i < nOwners; i++ {
			kind := ownerKinds[rnd.Intn(len(ownerKinds))]
			owners = append(owners, api.CoreOwnerSpec{
				UserSpec: api.UserSpec{
					Name: randomName(rnd, 3+rnd.Intn(4)), // length 3–6
					Kind: kind,
				},
			})
		}

		for lookupIdx := 0; lookupIdx < numLookupsPerList; lookupIdx++ {
			// Generate a random userName and groups,
			// sometimes biased to hit existing owners/groups.
			var userName string
			var groups []string

			// 50% of the time: pick an existing owner name as userName
			if len(owners) > 0 && rnd.Float64() < 0.5 {
				pick := owners[rnd.Intn(len(owners))]
				userName = pick.Name
			} else {
				userName = randomName(rnd, 3+rnd.Intn(4))
			}

			// Random groups, sometimes including owner names
			nGroups := rnd.Intn(maxGroupsPerUser)
			for i := 0; i < nGroups; i++ {
				if len(owners) > 0 && rnd.Float64() < 0.5 {
					pick := owners[rnd.Intn(len(owners))]
					groups = append(groups, pick.Name)
				} else {
					groups = append(groups, randomName(rnd, 3+rnd.Intn(4)))
				}
			}

			got := owners.IsOwner(userName, groups)
			want := slowIsOwner(owners, userName, groups)

			if got != want {
				t.Fatalf("list=%d lookup=%d: mismatch\n  owners=%v\n  user=%q\n  groups=%v\n  optimized=%v\n  slow=%v",
					listIdx, lookupIdx, owners, userName, groups, got, want)
			}
		}
	}
}
