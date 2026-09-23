// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

//nolint:dupl
package api_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/projectcapsule/capsule/pkg/api"
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
			// Entries differing only by case must all be found, while the
			// match itself stays case-sensitive.
			[]string{"nginx", "NGINX", "traefik"},
			[]string{"nginx", "NGINX", "traefik"},
			[]string{"Nginx", "haproxy"},
		},
		{
			nil,
			nil,
			[]string{"any", "value"},
		},
	} {
		a := api.AllowedListSpec{
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
		a := api.AllowedListSpec{
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

func TestAllowedListSpec_Match(t *testing.T) {
	t.Parallel()

	spec := api.AllowedListSpec{
		Exact: []string{"exact"},
		Regex: "regex-.*",
	}

	assert.True(t, spec.Match("exact"))
	assert.True(t, spec.Match("regex-value"))
	assert.False(t, spec.Match("other"))
}

func TestDefaultAllowedListSpec_MatchDefault(t *testing.T) {
	t.Parallel()

	spec := api.DefaultAllowedListSpec{Default: "standard"}

	assert.True(t, spec.MatchDefault("standard"))
	assert.False(t, spec.MatchDefault("premium"))
}

func TestSelectorAllowedListSpec(t *testing.T) {
	t.Parallel()

	obj := &corev1.ConfigMap{
		Name:   "by-name",
		Labels: map[string]string{"tier": "backend"}}

	byName := api.SelectorAllowedListSpec{
		Exact: []string{"by-name"},
	}
	assert.True(t, byName.MatchSelectByName(obj))

	bySelector := api.SelectorAllowedListSpec{
		MatchLabels: map[string]string{"tier": "backend"},
	}
	assert.True(t, bySelector.SelectorMatch(obj))
	assert.True(t, bySelector.MatchSelectByName(obj))

	invalidSelector := api.SelectorAllowedListSpec{
		MatchExpressions: []metav1.LabelSelectorRequirement{{Key: "tier", Operator: "invalid"}},
	}
	assert.False(t, invalidSelector.SelectorMatch(obj))
	assert.False(t, invalidSelector.MatchSelectByName(nil))
}

func TestSelectionListWithDefaultSpec(t *testing.T) {
	t.Parallel()

	spec := api.SelectionListWithDefaultSpec{Default: "standard"}
	assert.True(t, spec.MatchDefault("standard"))
	assert.False(t, spec.MatchDefault("premium"))

	obj := &corev1.ConfigMap{
		Labels: map[string]string{"class": "gold"}}

	selector := api.SelectionListWithSpec{
		MatchLabels: map[string]string{"class": "gold"},
	}
	assert.True(t, selector.SelectorMatch(obj))
	assert.False(t, selector.SelectorMatch(nil))
}
