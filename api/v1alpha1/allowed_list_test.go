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
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestAllowedListSpec_ExactMatch(t *testing.T) {
	type tc struct {
		In    []string
		True  []string
		False []string
	}
	for _, tc := range []tc{
		{
			[]string{"foo", "bar", "bizz", "buzz"},
			[]string{"foo", "bar", "bizz", "buzz"},
			[]string{"bing", "bong"},
		},
		{
			[]string{"one", "two", "three"},
			[]string{"one", "two", "three"},
			[]string{"a", "b", "c"},
		},
		{
			nil,
			nil,
			[]string{"any", "value"},
		},
	} {
		a := AllowedListSpec{
			Exact: tc.In,
		}
		for _, ok := range tc.True {
			assert.True(t, a.ExactMatch(ok))
		}
		for _, ko := range tc.False {
			assert.False(t, a.ExactMatch(ko))
		}
	}
}

func TestAllowedListSpec_RegexMatch(t *testing.T) {
	type tc struct {
		Regex string
		True  []string
		False []string
	}
	for _, tc := range []tc{
		{`first-\w+-pattern`, []string{"first-date-pattern", "first-year-pattern"}, []string{"broken", "first-year", "second-date-pattern"}},
		{``, nil, []string{"any", "value"}},
	} {
		a := AllowedListSpec{
			Regex: tc.Regex,
		}
		for _, ok := range tc.True {
			assert.True(t, a.RegexMatch(ok))
		}
		for _, ko := range tc.False {
			assert.False(t, a.RegexMatch(ko))
		}
	}
}
