// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package api

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
)

// +kubebuilder:object:generate=true
type ForbiddenListSpec struct {
	Exact []string `json:"denied,omitempty"`
	Regex string   `json:"deniedRegex,omitempty"`
}

func (in ForbiddenListSpec) ExactMatch(value string) bool {
	return slices.Contains(in.Exact, value)
}

func (in ForbiddenListSpec) RegexMatch(value string) (ok bool) {
	if len(in.Regex) > 0 {
		r, err := regexp.Compile(in.Regex)
		if err != nil {
			// This boolean API cannot report configuration errors. Fail closed so
			// malformed deny patterns cannot silently disable metadata protection.
			return true
		}

		ok = r.MatchString(value)
	}

	return ok
}

type ForbiddenError struct {
	key  string
	spec ForbiddenListSpec
}

func NewForbiddenError(key string, forbiddenSpec ForbiddenListSpec) error {
	return &ForbiddenError{
		key:  key,
		spec: forbiddenSpec,
	}
}

func (f ForbiddenError) Error() string {
	return fmt.Sprintf("%s is forbidden for the current Tenant. %s", f.key, f.appendForbiddenError())
}

//nolint:predeclared,revive
func (f *ForbiddenError) appendForbiddenError() (append string) {
	append += "Forbidden are "
	if len(f.spec.Exact) > 0 {
		append += fmt.Sprintf("one of the following (%s)", strings.Join(f.spec.Exact, ", "))
		if len(f.spec.Regex) > 0 {
			append += " or "
		}
	}

	if len(f.spec.Regex) > 0 {
		append += fmt.Sprintf("matching the regex %s", f.spec.Regex)
	}

	return append
}

func ValidateForbidden(metadata map[string]string, forbiddenList ForbiddenListSpec) error {
	if len(metadata) == 0 || (len(forbiddenList.Exact) == 0 && forbiddenList.Regex == "") {
		return nil
	}

	var expression *regexp.Regexp

	if forbiddenList.Regex != "" {
		var err error

		// Compile once per metadata map, preserving literal whitespace in legacy
		// patterns. The rules regex cache normalizes whitespace and is not equivalent.
		expression, err = regexp.Compile(forbiddenList.Regex)
		if err != nil {
			return fmt.Errorf("invalid forbidden metadata regex %q: %w", forbiddenList.Regex, err)
		}
	}

	for key := range metadata {
		if forbiddenList.ExactMatch(key) || (expression != nil && expression.MatchString(key)) {
			return NewForbiddenError(
				key,
				forbiddenList,
			)
		}
	}

	return nil
}
