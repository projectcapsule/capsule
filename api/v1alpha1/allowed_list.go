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

package v1alpha1

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
