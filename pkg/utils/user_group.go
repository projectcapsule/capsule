/*
Copyright 2020 Clastix Labs.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

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
	for k, v := range groups {
		list[k] = v
	}
	sort.SliceStable(list, func(i, j int) bool {
		return list[i] < list[j]
	})
	return list
}

// Find sorts itself using the SliceStable and perform a binary-search for the given string.
func (u userGroupList) Find(needle string) (found bool) {
	sort.SliceStable(u, func(i, j int) bool {
		return i < j
	})
	i := sort.SearchStrings(u, needle)
	found = i < len(u) && u[i] == needle
	return
}
