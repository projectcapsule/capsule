// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package pvc

import (
	"fmt"
	"strings"

	capsulev1beta1 "github.com/clastix/capsule/api/v1beta1"
)

type storageClassNotValid struct {
	spec capsulev1beta1.AllowedListSpec
}

func NewStorageClassNotValid(storageClasses capsulev1beta1.AllowedListSpec) error {
	return &storageClassNotValid{
		spec: storageClasses,
	}
}

func appendError(spec capsulev1beta1.AllowedListSpec) (append string) {
	if len(spec.Exact) > 0 {
		append += fmt.Sprintf(", one of the following (%s)", strings.Join(spec.Exact, ", "))
	}
	if len(spec.Regex) > 0 {
		append += fmt.Sprintf(", or matching the regex %s", spec.Regex)
	}
	return
}

func (s storageClassNotValid) Error() (err string) {
	return "A valid Storage Class must be used" + appendError(s.spec)
}

type storageClassForbidden struct {
	className string
	spec      capsulev1beta1.AllowedListSpec
}

func NewStorageClassForbidden(className string, storageClasses capsulev1beta1.AllowedListSpec) error {
	return &storageClassForbidden{
		className: className,
		spec:      storageClasses,
	}
}

func (f storageClassForbidden) Error() string {
	return fmt.Sprintf("Storage Class %s is forbidden for the current Tenant%s", f.className, appendError(f.spec))
}
