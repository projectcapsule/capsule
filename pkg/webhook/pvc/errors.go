// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package pvc

import (
	"fmt"

	"github.com/clastix/capsule/pkg/api"
	"github.com/clastix/capsule/pkg/webhook/utils"
)

type storageClassNotValidError struct {
	spec api.DefaultAllowedListSpec
}

func NewStorageClassNotValid(storageClasses api.DefaultAllowedListSpec) error {
	return &storageClassNotValidError{
		spec: storageClasses,
	}
}

func (s storageClassNotValidError) Error() (err string) {
	msg := "A valid Storage Class must be used: "

	return utils.DefaultAllowedValuesErrorMessage(s.spec, msg)
}

type storageClassForbiddenError struct {
	className string
	spec      api.DefaultAllowedListSpec
}

func NewStorageClassForbidden(className string, storageClasses api.DefaultAllowedListSpec) error {
	return &storageClassForbiddenError{
		className: className,
		spec:      storageClasses,
	}
}

func (f storageClassForbiddenError) Error() string {
	msg := fmt.Sprintf("Storage Class %s is forbidden for the current Tenant ", f.className)

	return utils.DefaultAllowedValuesErrorMessage(f.spec, msg)
}
