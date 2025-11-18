// Copyright 2020-2025 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

//nolint:dupl
package api

import (
	"sort"
)

// +kubebuilder:object:generate=true

type UserListSpec []UserSpec

func (u UserListSpec) IsPresent(name string, groups []string) bool {
	for _, user := range u {
		switch user.Kind {
		case UserOwner, ServiceAccountOwner:
			if name == user.Name {
				return true
			}
		case GroupOwner:
			for _, group := range groups {
				if group == user.Name {
					return true
				}
			}
		}
	}

	return false
}

func (o UserListSpec) FindUser(name string, kind OwnerKind) (owner UserSpec) {
	sort.Sort(ByKindName(o))
	i := sort.Search(len(o), func(i int) bool {
		return o[i].Kind >= kind && o[i].Name >= name
	})

	if i < len(o) && o[i].Kind == kind && o[i].Name == name {
		return o[i]
	}

	return owner
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
