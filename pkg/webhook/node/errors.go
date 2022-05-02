// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package node

import (
	"fmt"
	"strings"

	capsulev1beta1 "github.com/clastix/capsule/api/v1beta1"
)

// nolint:predeclared
func appendForbiddenError(spec *capsulev1beta1.ForbiddenListSpec) (append string) {
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

	return
}

type nodeLabelForbiddenError struct {
	spec *capsulev1beta1.ForbiddenListSpec
}

func NewNodeLabelForbiddenError(forbiddenSpec *capsulev1beta1.ForbiddenListSpec) error {
	return &nodeLabelForbiddenError{
		spec: forbiddenSpec,
	}
}

func (f nodeLabelForbiddenError) Error() string {
	return fmt.Sprintf("Unable to update node as some labels are marked as forbidden by system administrator. %s", appendForbiddenError(f.spec))
}

type nodeAnnotationForbiddenError struct {
	spec *capsulev1beta1.ForbiddenListSpec
}

func NewNodeAnnotationForbiddenError(forbiddenSpec *capsulev1beta1.ForbiddenListSpec) error {
	return &nodeAnnotationForbiddenError{
		spec: forbiddenSpec,
	}
}

func (f nodeAnnotationForbiddenError) Error() string {
	return fmt.Sprintf("Unable to update node as some annotations are marked as forbidden by system administrator. %s", appendForbiddenError(f.spec))
}
