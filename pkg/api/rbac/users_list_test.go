// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package rbac_test

import (
	"math/rand"
	"reflect"
	"sort"
	"testing"
	"time"

	"github.com/projectcapsule/capsule/pkg/api/rbac"
)

func linearFindUser(list rbac.UserListSpec, name string, kind rbac.OwnerKind) (rbac.UserSpec, bool) {
	for _, u := range list {
		if u.Kind == kind && u.Name == name {
			return u, true
		}
	}
	return rbac.UserSpec{}, false
}

func slowIsPresent(u rbac.UserListSpec, name string, groups []string) bool {
	for _, user := range u {
		switch user.Kind {
		case rbac.UserOwner, rbac.ServiceAccountOwner:
			if name == user.Name {
				return true
			}
		case rbac.GroupOwner:
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
	u := rbac.UserListSpec{
		rbac.UserSpec{Name: "b", Kind: rbac.ServiceAccountOwner},
		rbac.UserSpec{Name: "z", Kind: rbac.UserOwner},
		rbac.UserSpec{Name: "a", Kind: rbac.GroupOwner},
		rbac.UserSpec{Name: "a", Kind: rbac.UserOwner},
	}

	// Sort using production ordering
	got := append(rbac.UserListSpec(nil), u...)
	sort.Sort(rbac.ByKindName(got))

	// Manually sorted expectation using the same logic.
	want := append(rbac.UserListSpec(nil), u...)
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

	ownerKinds := []rbac.OwnerKind{
		rbac.GroupOwner,
		rbac.UserOwner,
		rbac.ServiceAccountOwner,
	}

	const (
		numLists          = 200
		maxLength         = 40
		numLookupsPerList = 80
	)

	for listIdx := 0; listIdx < numLists; listIdx++ {
		var list rbac.UserListSpec
		n := rnd.Intn(maxLength)
		for i := 0; i < n; i++ {
			k := ownerKinds[rnd.Intn(len(ownerKinds))]
			list = append(list, rbac.UserSpec{
				Name: randomName(rnd, 3+rnd.Intn(4)), // length 3–6
				Kind: k,
			})
		}

		for lookupIdx := 0; lookupIdx < numLookupsPerList; lookupIdx++ {
			var qName string
			var qKind rbac.OwnerKind

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

			listCopy := append(rbac.UserListSpec(nil), list...) // FindUser sorts in-place
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

	ownerKinds := []rbac.OwnerKind{
		rbac.UserOwner,
		rbac.GroupOwner,
		rbac.ServiceAccountOwner,
	}

	const (
		numLists          = 200
		maxOwnersPerList  = 30
		numLookupsPerList = 80
		maxGroupsPerUser  = 10
	)

	for listIdx := 0; listIdx < numLists; listIdx++ {
		// Generate a random user list (possibly with duplicates).
		var users rbac.UserListSpec
		nOwners := rnd.Intn(maxOwnersPerList)
		for i := 0; i < nOwners; i++ {
			kind := ownerKinds[rnd.Intn(len(ownerKinds))]
			users = append(users, rbac.UserSpec{
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
	users := rbac.UserListSpec{
		rbac.UserSpec{Name: "alice", Kind: rbac.UserOwner},
		rbac.UserSpec{Name: "svc-1", Kind: rbac.ServiceAccountOwner},
		rbac.UserSpec{Name: "team-a", Kind: rbac.GroupOwner},
		rbac.UserSpec{Name: "bob", Kind: rbac.UserOwner},
		rbac.UserSpec{Name: "team-b", Kind: rbac.GroupOwner},
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
	gotUsers := users.GetByKinds([]rbac.OwnerKind{rbac.UserOwner})
	wantUsers := []string{"alice", "bob"}
	if !eqStrings(gotUsers, wantUsers) {
		t.Fatalf("GetByKinds([UserOwner]) = %v, want %v", gotUsers, wantUsers)
	}

	// Single kind: GroupOwner
	gotGroups := users.GetByKinds([]rbac.OwnerKind{rbac.GroupOwner})
	wantGroups := []string{"team-a", "team-b"}
	if !eqStrings(gotGroups, wantGroups) {
		t.Fatalf("GetByKinds([GroupOwner]) = %v, want %v", gotGroups, wantGroups)
	}

	// Multiple kinds: UserOwner + ServiceAccountOwner
	gotUsersAndSAs := users.GetByKinds([]rbac.OwnerKind{rbac.UserOwner, rbac.ServiceAccountOwner})
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
	gotUnknown := users.GetByKinds([]rbac.OwnerKind{rbac.OwnerKind("does-not-exist")})
	if gotUnknown != nil {
		t.Fatalf("GetByKinds([unknown]) = %v, want nil", gotUnknown)
	}
}

func TestGetByKinds_Randomized(t *testing.T) {
	rnd := rand.New(rand.NewSource(123))

	ownerKinds := []rbac.OwnerKind{
		rbac.UserOwner,
		rbac.GroupOwner,
		rbac.ServiceAccountOwner,
	}

	const (
		numLists         = 200
		maxOwnersPerList = 50
	)

	for listIdx := 0; listIdx < numLists; listIdx++ {
		var users rbac.UserListSpec
		n := rnd.Intn(maxOwnersPerList)
		for i := 0; i < n; i++ {
			k := ownerKinds[rnd.Intn(len(ownerKinds))]
			users = append(users, rbac.UserSpec{
				Name: randomName(rnd, 3+rnd.Intn(4)), // reuse your helper
				Kind: k,
			})
		}

		// Try several random kind-subsets per list
		for subsetIdx := 0; subsetIdx < 10; subsetIdx++ {
			// Build a random subset of kinds
			var kinds []rbac.OwnerKind
			for _, k := range ownerKinds {
				if rnd.Float64() < 0.5 {
					kinds = append(kinds, k)
				}
			}

			got := users.GetByKinds(kinds)

			// Reference implementation: filter + sort
			kindSet := make(map[rbac.OwnerKind]struct{}, len(kinds))
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

func TestUserListSpec_SplitUsersAndGroups(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		in         rbac.UserListSpec
		wantUsers  []string
		wantGroups []string
	}{
		{
			name:       "nil",
			in:         nil,
			wantUsers:  nil,
			wantGroups: nil,
		},
		{
			name:       "empty",
			in:         rbac.UserListSpec{},
			wantUsers:  nil,
			wantGroups: nil,
		},
		{
			name: "users_only_sorted_and_deduped",
			in: rbac.UserListSpec{
				{Kind: rbac.UserOwner, Name: "zara"},
				{Kind: rbac.UserOwner, Name: "alice"},
				{Kind: rbac.UserOwner, Name: "alice"},
				{Kind: rbac.UserOwner, Name: "bob"},
			},
			wantUsers:  []string{"alice", "bob", "zara"},
			wantGroups: nil,
		},
		{
			name: "groups_only_sorted_and_deduped",
			in: rbac.UserListSpec{
				{Kind: rbac.GroupOwner, Name: "team-b"},
				{Kind: rbac.GroupOwner, Name: "team-a"},
				{Kind: rbac.GroupOwner, Name: "team-a"},
			},
			wantUsers:  nil,
			wantGroups: []string{"team-a", "team-b"},
		},
		{
			name: "mix_users_groups_and_serviceaccounts",
			in: rbac.UserListSpec{
				{Kind: rbac.GroupOwner, Name: "ops"},
				{Kind: rbac.ServiceAccountOwner, Name: "system:serviceaccount:ns:sa"},
				{Kind: rbac.UserOwner, Name: "alice"},
				{Kind: rbac.GroupOwner, Name: "dev"},
			},
			wantUsers:  []string{"alice", "system:serviceaccount:ns:sa"},
			wantGroups: []string{"dev", "ops"},
		},
		{
			name: "ignore_empty_names",
			in: rbac.UserListSpec{
				{Kind: rbac.UserOwner, Name: ""},
				{Kind: rbac.GroupOwner, Name: ""},
				{Kind: rbac.ServiceAccountOwner, Name: ""},
				{Kind: rbac.UserOwner, Name: "alice"},
			},
			wantUsers:  []string{"alice"},
			wantGroups: nil,
		},
		{
			name: "all_kinds",
			in: rbac.UserListSpec{
				{Kind: rbac.ServiceAccountOwner, Name: "x"},
				{Kind: rbac.UserOwner, Name: "alice"},
				{Kind: rbac.GroupOwner, Name: "dev"},
			},
			wantUsers:  []string{"alice", "x"},
			wantGroups: []string{"dev"},
		},
		{
			name: "same_name_in_user_and_group_goes_to_respective_buckets",
			in: rbac.UserListSpec{
				{Kind: rbac.UserOwner, Name: "same"},
				{Kind: rbac.GroupOwner, Name: "same"},
			},
			wantUsers:  []string{"same"},
			wantGroups: []string{"same"},
		},
		{
			name: "deterministic_ordering_with_many_values",
			in: rbac.UserListSpec{
				{Kind: rbac.UserOwner, Name: "c"},
				{Kind: rbac.UserOwner, Name: "a"},
				{Kind: rbac.UserOwner, Name: "b"},
				{Kind: rbac.GroupOwner, Name: "g2"},
				{Kind: rbac.GroupOwner, Name: "g1"},
			},
			wantUsers:  []string{"a", "b", "c"},
			wantGroups: []string{"g1", "g2"},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			gotUsers, gotGroups := tt.in.SplitUsersAndGroups()

			if !reflect.DeepEqual(gotUsers, tt.wantUsers) {
				t.Fatalf("users = %#v, want %#v", gotUsers, tt.wantUsers)
			}
			if !reflect.DeepEqual(gotGroups, tt.wantGroups) {
				t.Fatalf("groups = %#v, want %#v", gotGroups, tt.wantGroups)
			}
		})
	}
}

func TestUserListSpec_SplitUsersAndGroups_Idempotent(t *testing.T) {
	t.Parallel()

	in := rbac.UserListSpec{
		{Kind: rbac.GroupOwner, Name: "team-b"},
		{Kind: rbac.UserOwner, Name: "zara"},
		{Kind: rbac.UserOwner, Name: "alice"},
		{Kind: rbac.GroupOwner, Name: "team-a"},
		{Kind: rbac.UserOwner, Name: "alice"},
	}

	u1, g1 := in.SplitUsersAndGroups()
	u2, g2 := in.SplitUsersAndGroups()

	if !reflect.DeepEqual(u1, u2) {
		t.Fatalf("users not deterministic: first=%#v second=%#v", u1, u2)
	}
	if !reflect.DeepEqual(g1, g2) {
		t.Fatalf("groups not deterministic: first=%#v second=%#v", g1, g2)
	}
}
