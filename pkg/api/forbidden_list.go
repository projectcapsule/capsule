// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package api

import (
	"regexp"
	"sort"
	"strings"
)

// +kubebuilder:object:generate=true

type ForbiddenListSpec struct {
	Exact []string `json:"denied,omitempty"`
	Regex string   `json:"deniedRegex,omitempty"`
}

func (in ForbiddenListSpec) ExactMatch(value string) (ok bool) {
	if len(in.Exact) > 0 {
		sort.SliceStable(in.Exact, func(i, j int) bool {
			return strings.ToLower(in.Exact[i]) < strings.ToLower(in.Exact[j])
		})

		i := sort.SearchStrings(in.Exact, value)

		ok = i < len(in.Exact) && in.Exact[i] == value
	}

	return
}

func (in ForbiddenListSpec) RegexMatch(value string) (ok bool) {
	if len(in.Regex) > 0 {
		ok = regexp.MustCompile(in.Regex).MatchString(value)
	}

	return
}
