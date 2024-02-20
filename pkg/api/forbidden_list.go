// Copyright 2020-2023 Project Capsule Authors.
// SPDX-License-Identifier: Apache-2.0

package api

import (
	"fmt"
	"reflect"
	"regexp"
	"sort"
	"strings"
)

const (
	// ForbiddenLabelReason used as reason string to deny forbidden labels.
	ForbiddenLabelReason = "ForbiddenLabel"
	// ForbiddenAnnotationReason used as reason string to deny forbidden annotations.
	ForbiddenAnnotationReason = "ForbiddenAnnotation"
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

	return
}

func (f ForbiddenError) Error() string {
	return fmt.Sprintf("%s is forbidden for the current Tenant. %s", f.key, f.appendForbiddenError())
}

func ValidateForbidden(metadata map[string]string, forbiddenList ForbiddenListSpec) error {
	if reflect.DeepEqual(ForbiddenListSpec{}, forbiddenList) {
		return nil
	}

	for key := range metadata {
		var forbidden, matched bool
		forbidden = forbiddenList.ExactMatch(key)
		matched = forbiddenList.RegexMatch(key)

		if forbidden || matched {
			return NewForbiddenError(
				key,
				forbiddenList,
			)
		}
	}

	return nil
}
