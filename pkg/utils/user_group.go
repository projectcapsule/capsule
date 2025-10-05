// Copyright 2020-2025 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package utils

import (
	"sort"
)

type UserGroupList interface {
	Find(needle string) (found bool)
}

type userGroupList []string

func NewUserGroupList(groups []string) UserGroupList {
	list := make(userGroupList, len(groups))
	copy(list, groups)

	sort.SliceStable(list, func(i, j int) bool {
		return list[i] < list[j]
	})

	return list
}

// Find sorts itself using the SliceStable and perform a binary-search for the given string.
func (u userGroupList) Find(needle string) (found bool) {
	i := sort.SearchStrings(u, needle)

	found = i < len(u) && u[i] == needle

	return found
}
