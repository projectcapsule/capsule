// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package api_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/projectcapsule/capsule/pkg/api"
)

func denied() api.ForbiddenListSpec {
	return api.ForbiddenListSpec{
		Exact: []string{
			"kubernetes.io/metadata.name",
			"pod-security.kubernetes.io/enforce",
			"NetworkPolicy",
		},
	}
}

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
		a := api.ForbiddenListSpec{
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
		a := api.ForbiddenListSpec{
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
		ForbiddenSpec api.ForbiddenListSpec
		HasError      bool
	}

	for _, tc := range []tc{
		{
			Keys: map[string]string{"foobar": "", "thesecondkey": "", "anotherkey": ""},
			ForbiddenSpec: api.ForbiddenListSpec{
				Exact: []string{"foobar", "somelabelkey1"},
			},
			HasError: true,
		},
		{
			Keys: map[string]string{"foobar": ""},
			ForbiddenSpec: api.ForbiddenListSpec{
				Exact: []string{"foobar.io", "somelabelkey1", "test-exact"},
			},
			HasError: false,
		},
		{
			Keys: map[string]string{"foobar": "", "barbaz": ""},
			ForbiddenSpec: api.ForbiddenListSpec{
				Regex: "foo.*",
			},
			HasError: true,
		},
		{
			Keys: map[string]string{"foobar": "", "another-annotation-key": ""},
			ForbiddenSpec: api.ForbiddenListSpec{
				Regex: "foo1111",
			},
			HasError: false,
		},
	} {
		if tc.HasError {
			assert.Error(t, api.ValidateForbidden(tc.Keys, tc.ForbiddenSpec))
		}

		if !tc.HasError {
			assert.NoError(t, api.ValidateForbidden(tc.Keys, tc.ForbiddenSpec))
		}
	}
}

func TestForbiddenKeysBypassed(t *testing.T) {
	for _, k := range []string{"NetworkPolicy", "kubernetes.io/metadata.name"} {
		if err := api.ValidateForbidden(map[string]string{k: "owned"}, denied()); err == nil {
			t.Errorf("BYPASS CONFIRMED: ValidateForbidden ALLOWED denied key %q (list=%v)", k, denied().Exact)
		} else {
			t.Logf("(no bypass) correctly denied %q: %v", k, err)
		}
	}
}

// Positive control: a third denied key in the SAME list is still correctly
// blocked — proving the policy genuinely forbids these keys and the harness is
// wired right (i.e. the bypass above is selective, not a dead enforcement path).
func TestPositiveControl_StillBlocked(t *testing.T) {
	if err := api.ValidateForbidden(map[string]string{"pod-security.kubernetes.io/enforce": "privileged"}, denied()); err == nil {
		t.Errorf("control failure: denied key 'pod-security.kubernetes.io/enforce' was NOT blocked")
	}
}

// Negative control: a key the admin did NOT deny is correctly allowed,
// proving the webhook is not simply denying everything.
func TestPoC_NegativeControl_BenignAllowed(t *testing.T) {
	if err := api.ValidateForbidden(map[string]string{"app.kubernetes.io/name": "frontend"}, denied()); err != nil {
		t.Errorf("control failure: benign key was wrongly denied: %v", err)
	}
}

// Direct primitive check, minimal repro of the root cause.
func TestExactMatch_RootCause(t *testing.T) {
	spec := api.ForbiddenListSpec{Exact: []string{"B", "a"}} // mixed case
	if !spec.ExactMatch("B") {
		t.Errorf("ROOT CAUSE: ExactMatch(%q) returned false though %q is in %v", "B", "B", spec.Exact)
	}
}
