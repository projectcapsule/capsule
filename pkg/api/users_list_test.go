// Copyright 2020-2026 Project Capsule Authors
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

func linearFindUser(list api.UserListSpec, name string, kind api.OwnerKind) (api.UserSpec, bool) {
	for _, u := range list {
		if u.Kind == kind && u.Name == name {
			return u, true
		}
	}
	return api.UserSpec{}, false
}

func slowIsPresent(u api.UserListSpec, name string, groups []string) bool {
	for _, user := range u {
		switch user.Kind {
		case api.UserOwner, api.ServiceAccountOwner:
			if name == user.Name {
				return true
			}
		case api.GroupOwner:
			for _, group := range groups {
				if group == user.Name {
					return true
				}
			}
		}
	}
	return false
}

func TestByKindNameOrdering_UserListSpec(t *testing.T) {
	u := api.UserListSpec{
		api.UserSpec{Name: "b", Kind: api.ServiceAccountOwner},
		api.UserSpec{Name: "z", Kind: api.UserOwner},
		api.UserSpec{Name: "a", Kind: api.GroupOwner},
		api.UserSpec{Name: "a", Kind: api.UserOwner},
	}

	// Sort using production ordering
	got := append(api.UserListSpec(nil), u...)
	sort.Sort(api.ByKindName(got))

	// Manually sorted expectation using the same logic.
	want := append(api.UserListSpec(nil), u...)
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

func TestFindUser_Randomized(t *testing.T) {
	rnd := rand.New(rand.NewSource(42))

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
		var list api.UserListSpec
		n := rnd.Intn(maxLength)
		for i := 0; i < n; i++ {
			k := ownerKinds[rnd.Intn(len(ownerKinds))]
			list = append(list, api.UserSpec{
				Name: randomName(rnd, 3+rnd.Intn(4)), // length 3–6
				Kind: k,
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

			listCopy := append(api.UserListSpec(nil), list...) // FindUser sorts in-place
			gotUser, gotFound := listCopy.FindUser(qName, qKind)
			wantUser, wantFound := linearFindUser(list, qName, qKind)

			if gotFound != wantFound {
				t.Fatalf("list=%d lookup=%d: found mismatch for (%q,%v): got=%v, want=%v",
					listIdx, lookupIdx, qName, qKind, gotFound, wantFound)
			}
			if gotFound && !reflect.DeepEqual(gotUser, wantUser) {
				t.Fatalf("list=%d lookup=%d: user mismatch for (%q,%v):\n got= %+v\nwant= %+v",
					listIdx, lookupIdx, qName, qKind, gotUser, wantUser)
			}
		}
	}
}

func TestIsPresent_RandomizedMatchesSlowImplementation(t *testing.T) {
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
		// Generate a random user list (possibly with duplicates).
		var users api.UserListSpec
		nOwners := rnd.Intn(maxOwnersPerList)
		for i := 0; i < nOwners; i++ {
			kind := ownerKinds[rnd.Intn(len(ownerKinds))]
			users = append(users, api.UserSpec{
				Name: randomName(rnd, 3+rnd.Intn(4)), // length 3–6
				Kind: kind,
			})
		}

		for lookupIdx := 0; lookupIdx < numLookupsPerList; lookupIdx++ {
			// Generate a random userName and groups,
			// sometimes biased to hit existing owners/groups.
			var userName string
			var groups []string

			// 50% of the time: pick an existing owner name as userName
			if len(users) > 0 && rnd.Float64() < 0.5 {
				pick := users[rnd.Intn(len(users))]
				userName = pick.Name
			} else {
				userName = randomName(rnd, 3+rnd.Intn(4))
			}

			// Random groups, sometimes including owner names
			nGroups := rnd.Intn(maxGroupsPerUser)
			for i := 0; i < nGroups; i++ {
				if len(users) > 0 && rnd.Float64() < 0.5 {
					pick := users[rnd.Intn(len(users))]
					groups = append(groups, pick.Name)
				} else {
					groups = append(groups, randomName(rnd, 3+rnd.Intn(4)))
				}
			}

			got := users.IsPresent(userName, groups)
			want := slowIsPresent(users, userName, groups)

			if got != want {
				t.Fatalf("list=%d lookup=%d: mismatch\n  users=%v\n  user=%q\n  groups=%v\n  optimized=%v\n  slow=%v",
					listIdx, lookupIdx, users, userName, groups, got, want)
			}
		}
	}
}

func TestGetByKinds_Basic(t *testing.T) {
	users := api.UserListSpec{
		api.UserSpec{Name: "alice", Kind: api.UserOwner},
		api.UserSpec{Name: "svc-1", Kind: api.ServiceAccountOwner},
		api.UserSpec{Name: "team-a", Kind: api.GroupOwner},
		api.UserSpec{Name: "bob", Kind: api.UserOwner},
		api.UserSpec{Name: "team-b", Kind: api.GroupOwner},
	}

	eqStrings := func(got, want []string) bool {
		if got == nil && want == nil {
			return true
		}
		if len(got) != len(want) {
			return false
		}
		sort.Strings(got)
		sort.Strings(want)
		for i := range got {
			if got[i] != want[i] {
				return false
			}
		}
		return true
	}

	// Single kind: UserOwner
	gotUsers := users.GetByKinds([]api.OwnerKind{api.UserOwner})
	wantUsers := []string{"alice", "bob"}
	if !eqStrings(gotUsers, wantUsers) {
		t.Fatalf("GetByKinds([UserOwner]) = %v, want %v", gotUsers, wantUsers)
	}

	// Single kind: GroupOwner
	gotGroups := users.GetByKinds([]api.OwnerKind{api.GroupOwner})
	wantGroups := []string{"team-a", "team-b"}
	if !eqStrings(gotGroups, wantGroups) {
		t.Fatalf("GetByKinds([GroupOwner]) = %v, want %v", gotGroups, wantGroups)
	}

	// Multiple kinds: UserOwner + ServiceAccountOwner
	gotUsersAndSAs := users.GetByKinds([]api.OwnerKind{api.UserOwner, api.ServiceAccountOwner})
	wantUsersAndSAs := []string{"alice", "bob", "svc-1"}
	if !eqStrings(gotUsersAndSAs, wantUsersAndSAs) {
		t.Fatalf("GetByKinds([UserOwner,ServiceAccountOwner]) = %v, want %v",
			gotUsersAndSAs, wantUsersAndSAs)
	}

	// No kinds → nil
	gotNone := users.GetByKinds(nil)
	if gotNone != nil {
		t.Fatalf("GetByKinds(nil) = %v, want nil", gotNone)
	}

	// Kind not present at all
	gotUnknown := users.GetByKinds([]api.OwnerKind{api.OwnerKind("does-not-exist")})
	if gotUnknown != nil {
		t.Fatalf("GetByKinds([unknown]) = %v, want nil", gotUnknown)
	}
}

func TestGetByKinds_Randomized(t *testing.T) {
	rnd := rand.New(rand.NewSource(123))

	ownerKinds := []api.OwnerKind{
		api.UserOwner,
		api.GroupOwner,
		api.ServiceAccountOwner,
	}

	const (
		numLists         = 200
		maxOwnersPerList = 50
	)

	for listIdx := 0; listIdx < numLists; listIdx++ {
		var users api.UserListSpec
		n := rnd.Intn(maxOwnersPerList)
		for i := 0; i < n; i++ {
			k := ownerKinds[rnd.Intn(len(ownerKinds))]
			users = append(users, api.UserSpec{
				Name: randomName(rnd, 3+rnd.Intn(4)), // reuse your helper
				Kind: k,
			})
		}

		// Try several random kind-subsets per list
		for subsetIdx := 0; subsetIdx < 10; subsetIdx++ {
			// Build a random subset of kinds
			var kinds []api.OwnerKind
			for _, k := range ownerKinds {
				if rnd.Float64() < 0.5 {
					kinds = append(kinds, k)
				}
			}

			got := users.GetByKinds(kinds)

			// Reference implementation: filter + sort
			kindSet := make(map[api.OwnerKind]struct{}, len(kinds))
			for _, k := range kinds {
				kindSet[k] = struct{}{}
			}

			var want []string
			if len(kinds) > 0 {
				for _, u := range users {
					if _, ok := kindSet[u.Kind]; ok {
						want = append(want, u.Name)
					}
				}
			}

			if len(want) == 0 {
				if got != nil {
					t.Fatalf("list=%d subset=%d: expected nil, got %v (kinds=%v, users=%v)",
						listIdx, subsetIdx, got, kinds, users)
				}
				continue
			}

			sort.Strings(want)
			sort.Strings(got)

			if !reflect.DeepEqual(got, want) {
				t.Fatalf("list=%d subset=%d: GetByKinds mismatch\n  kinds=%v\n  got=  %v\n  want= %v",
					listIdx, subsetIdx, kinds, got, want)
			}
		}
	}
}
