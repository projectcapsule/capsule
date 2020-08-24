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
	"strings"
)

type UserGroupList []string

func (u UserGroupList) Len() int {
	return len(u)
}

func (u UserGroupList) Less(i, j int) bool {
	return strings.ToLower(u[i]) < strings.ToLower(u[j])
}

func (u UserGroupList) Swap(i, j int) {
	u[i], u[j] = u[j], u[i]
}

func (u UserGroupList) IsInCapsuleGroup(capsuleGroup string) (ok bool) {
	sort.Sort(u)
	i := sort.SearchStrings(u, capsuleGroup)
	ok = i < u.Len() && u[i] == capsuleGroup
	return
}
