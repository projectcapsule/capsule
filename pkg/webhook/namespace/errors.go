// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package namespace

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

type namespaceQuotaExceededError struct{}

func NewNamespaceQuotaExceededError() error {
	return &namespaceQuotaExceededError{}
}

func (namespaceQuotaExceededError) Error() string {
	return "Cannot exceed Namespace quota: please, reach out to the system administrators"
}

type namespaceLabelForbiddenError struct {
	label string
	spec  *capsulev1beta1.ForbiddenListSpec
}

func NewNamespaceLabelForbiddenError(label string, forbiddenSpec *capsulev1beta1.ForbiddenListSpec) error {
	return &namespaceLabelForbiddenError{
		label: label,
		spec:  forbiddenSpec,
	}
}

func (f namespaceLabelForbiddenError) Error() string {
	return fmt.Sprintf("Label %s is forbidden for namespaces in the current Tenant. %s", f.label, appendForbiddenError(f.spec))
}

type namespaceAnnotationForbiddenError struct {
	annotation string
	spec       *capsulev1beta1.ForbiddenListSpec
}

func NewNamespaceAnnotationForbiddenError(annotation string, forbiddenSpec *capsulev1beta1.ForbiddenListSpec) error {
	return &namespaceAnnotationForbiddenError{
		annotation: annotation,
		spec:       forbiddenSpec,
	}
}

func (f namespaceAnnotationForbiddenError) Error() string {
	return fmt.Sprintf("Annotation %s is forbidden for namespaces in the current Tenant. %s", f.annotation, appendForbiddenError(f.spec))
}
