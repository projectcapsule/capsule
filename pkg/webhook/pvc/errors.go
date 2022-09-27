// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package pvc

import (
	"fmt"
	"strings"

	"github.com/clastix/capsule/pkg/api"
)

type storageClassNotValidError struct {
	spec api.AllowedListSpec
}

func NewStorageClassNotValid(storageClasses api.AllowedListSpec) error {
	return &storageClassNotValidError{
		spec: storageClasses,
	}
}

// nolint:predeclared
func appendError(spec api.AllowedListSpec) (append string) {
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
	spec      api.AllowedListSpec
}

func NewStorageClassForbidden(className string, storageClasses api.AllowedListSpec) error {
	return &storageClassForbiddenError{
		className: className,
		spec:      storageClasses,
	}
}

func (f storageClassForbiddenError) Error() string {
	return fmt.Sprintf("Storage Class %s is forbidden for the current Tenant%s", f.className, appendError(f.spec))
}
