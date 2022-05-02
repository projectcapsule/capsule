// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0
//nolint:dupl
package v1beta1

import (
	"regexp"
	"sort"
	"strings"
)

type AllowedListSpec struct {
	Exact []string `json:"allowed,omitempty"`
	Regex string   `json:"allowedRegex,omitempty"`
}

func (in *AllowedListSpec) ExactMatch(value string) (ok bool) {
	if len(in.Exact) > 0 {
		sort.SliceStable(in.Exact, func(i, j int) bool {
			return strings.ToLower(in.Exact[i]) < strings.ToLower(in.Exact[j])
		})

		i := sort.SearchStrings(in.Exact, value)

		ok = i < len(in.Exact) && in.Exact[i] == value
	}

	return
}

func (in AllowedListSpec) RegexMatch(value string) (ok bool) {
	if len(in.Regex) > 0 {
		ok = regexp.MustCompile(in.Regex).MatchString(value)
	}

	return
}
