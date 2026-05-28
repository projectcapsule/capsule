// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package users

func HasIgnoredGroup(userGroups []string, ignoredGroups []string) bool {
	if len(userGroups) == 0 || len(ignoredGroups) == 0 {
		return false
	}

	ignored := make(map[string]struct{}, len(ignoredGroups))
	for _, group := range ignoredGroups {
		ignored[group] = struct{}{}
	}

	for _, group := range userGroups {
		if _, ok := ignored[group]; ok {
			return true
		}
	}

	return false
}
