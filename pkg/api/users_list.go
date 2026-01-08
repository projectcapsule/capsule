// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package api

import (
	"sort"
)

// +kubebuilder:object:generate=true
type UserListSpec []UserSpec

func (o *UserListSpec) Upsert(newUser UserSpec) {
	users := *o

	// Comparator consistent with ByKindName
	less := func(a, b UserSpec) bool {
		ak, bk := a.Kind.String(), b.Kind.String()
		if ak != bk {
			return ak < bk
		}

		return a.Name < b.Name
	}

	// Ensure sorted before binary search
	sort.Sort(ByKindName(users))

	// Find first index where users[i] >= newUser
	idx := sort.Search(len(users), func(i int) bool {
		return !less(users[i], newUser)
	})

	// In this case merging for duplicates makes little sense as the values are identical
	if idx < len(users) && !less(newUser, users[idx]) && !less(users[idx], newUser) {
		return
	}

	users = append(users, newUser)
	sort.Sort(ByKindName(users))
	*o = users
}

func (u UserListSpec) IsPresent(name string, groups []string) bool {
	groupSet := make(map[string]struct{}, len(groups))
	for _, g := range groups {
		groupSet[g] = struct{}{}
	}

	for _, user := range u {
		switch user.Kind {
		case UserOwner, ServiceAccountOwner:
			if user.Name == name {
				return true
			}
		case GroupOwner:
			if _, ok := groupSet[user.Name]; ok {
				return true
			}
		}
	}

	return false
}

//nolint:dupl
func (o UserListSpec) FindUser(name string, kind OwnerKind) (UserSpec, bool) {
	sort.Sort(ByKindName(o))

	targetKind := kind.String()
	n := len(o)

	idx := sort.Search(n, func(i int) bool {
		ki := o[i].Kind.String()

		switch {
		case ki > targetKind:
			return true
		case ki < targetKind:
			return false
		default:
			return o[i].Name >= name
		}
	})

	if idx < n &&
		o[idx].Kind.String() == targetKind &&
		o[idx].Name == name {
		return o[idx], true
	}

	return UserSpec{}, false
}

func (o UserListSpec) GetByKinds(kinds []OwnerKind) []string {
	if len(o) == 0 || len(kinds) == 0 {
		return nil
	}

	kindSet := make(map[OwnerKind]struct{}, len(kinds))
	for _, k := range kinds {
		kindSet[k] = struct{}{}
	}

	names := make([]string, 0, len(o))

	for _, u := range o {
		if _, ok := kindSet[u.Kind]; ok {
			names = append(names, u.Name)
		}
	}

	if len(names) == 0 {
		return nil
	}

	sort.Strings(names)

	return names
}

type ByKindName UserListSpec

func (b ByKindName) Len() int {
	return len(b)
}

func (b ByKindName) Less(i, j int) bool {
	if b[i].Kind.String() != b[j].Kind.String() {
		return b[i].Kind.String() < b[j].Kind.String()
	}

	return b[i].Name < b[j].Name
}

func (b ByKindName) Swap(i, j int) {
	b[i], b[j] = b[j], b[i]
}
