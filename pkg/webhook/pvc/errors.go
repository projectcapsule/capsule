// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package pvc

import (
	"fmt"
	"strings"

	capsulev1beta1 "github.com/clastix/capsule/api/v1beta1"
)

type storageClassNotValidError struct {
	spec capsulev1beta1.AllowedListSpec
}

func NewStorageClassNotValid(storageClasses capsulev1beta1.AllowedListSpec) error {
	return &storageClassNotValidError{
		spec: storageClasses,
	}
}

// nolint:predeclared
func appendError(spec capsulev1beta1.AllowedListSpec) (append string) {
	if len(spec.Exact) > 0 {
		append += fmt.Sprintf(", one of the following (%s)", strings.Join(spec.Exact, ", "))
	}

	if len(spec.Regex) > 0 {
		append += fmt.Sprintf(", or matching the regex %s", spec.Regex)
	}

	return
}

func (s storageClassNotValidError) Error() (err string) {
	return "A valid Storage Class must be used" + appendError(s.spec)
}

type storageClassForbiddenError struct {
	className string
	spec      capsulev1beta1.AllowedListSpec
}

func NewStorageClassForbidden(className string, storageClasses capsulev1beta1.AllowedListSpec) error {
	return &storageClassForbiddenError{
		className: className,
		spec:      storageClasses,
	}
}

func (f storageClassForbiddenError) Error() string {
	return fmt.Sprintf("Storage Class %s is forbidden for the current Tenant%s", f.className, appendError(f.spec))
}
