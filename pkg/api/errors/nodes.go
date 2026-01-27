// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package errors

import (
	"fmt"
	"strings"

	capsulev1beta2 "github.com/projectcapsule/capsule/pkg/api"
)

//nolint:predeclared,revive
func appendForbiddenError(spec *capsulev1beta2.ForbiddenListSpec) (append string) {
	append += "Forbidden are "
	if len(spec.Exact) > 0 {
		append += fmt.Sprintf("one of the following (%s)", strings.Join(spec.Exact, ", "))
		if len(spec.Regex) > 0 {
			append += " or "
		}
	}

	if len(spec.Regex) > 0 {
		append += fmt.Sprintf("matching the regex %s", spec.Regex)
	}

	return append
}

type NodeLabelForbiddenError struct {
	spec *capsulev1beta2.ForbiddenListSpec
}

func NewNodeLabelForbiddenError(forbiddenSpec *capsulev1beta2.ForbiddenListSpec) error {
	return &NodeLabelForbiddenError{
		spec: forbiddenSpec,
	}
}

func (f NodeLabelForbiddenError) Error() string {
	return fmt.Sprintf("Unable to update node as some labels are marked as forbidden by system administrator. %s", appendForbiddenError(f.spec))
}

type NodeAnnotationForbiddenError struct {
	spec *capsulev1beta2.ForbiddenListSpec
}

func NewNodeAnnotationForbiddenError(forbiddenSpec *capsulev1beta2.ForbiddenListSpec) error {
	return &NodeAnnotationForbiddenError{
		spec: forbiddenSpec,
	}
}

func (f NodeAnnotationForbiddenError) Error() string {
	return fmt.Sprintf("Unable to update node as some annotations are marked as forbidden by system administrator. %s", appendForbiddenError(f.spec))
}
