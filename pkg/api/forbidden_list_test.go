// Copyright 2020-2023 Project Capsule Authors.
// SPDX-License-Identifier: Apache-2.0
//
//nolint:dupl
package api

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestForbiddenListSpec_ExactMatch(t *testing.T) {
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
		a := ForbiddenListSpec{
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

func TestForbiddenListSpec_RegexMatch(t *testing.T) {
	type tc struct {
		Regex string
		True  []string
		False []string
	}

	for _, tc := range []tc{
		{`first-\w+-pattern`, []string{"first-date-pattern", "first-year-pattern"}, []string{"broken", "first-year", "second-date-pattern"}},
		{``, nil, []string{"any", "value"}},
	} {
		a := ForbiddenListSpec{
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

func TestValidateForbidden(t *testing.T) {
	type tc struct {
		Keys          map[string]string
		ForbiddenSpec ForbiddenListSpec
		HasError      bool
	}

	for _, tc := range []tc{
		{
			Keys: map[string]string{"foobar": "", "thesecondkey": "", "anotherkey": ""},
			ForbiddenSpec: ForbiddenListSpec{
				Exact: []string{"foobar", "somelabelkey1"},
			},
			HasError: true,
		},
		{
			Keys: map[string]string{"foobar": ""},
			ForbiddenSpec: ForbiddenListSpec{
				Exact: []string{"foobar.io", "somelabelkey1", "test-exact"},
			},
			HasError: false,
		},
		{
			Keys: map[string]string{"foobar": "", "barbaz": ""},
			ForbiddenSpec: ForbiddenListSpec{
				Regex: "foo.*",
			},
			HasError: true,
		},
		{
			Keys: map[string]string{"foobar": "", "another-annotation-key": ""},
			ForbiddenSpec: ForbiddenListSpec{
				Regex: "foo1111",
			},
			HasError: false,
		},
	} {
		if tc.HasError {
			assert.Error(t, ValidateForbidden(tc.Keys, tc.ForbiddenSpec))
		}

		if !tc.HasError {
			assert.NoError(t, ValidateForbidden(tc.Keys, tc.ForbiddenSpec))
		}
	}
}
