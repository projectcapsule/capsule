// Copyright 2020-2025 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0
package dra

import (
	"fmt"

	"github.com/projectcapsule/capsule/internal/webhook/utils"
	"github.com/projectcapsule/capsule/pkg/api"
)

type deviceClassForbiddenError struct {
	deviceClassName string
	spec            api.SelectorAllowedListSpec
}

func (i deviceClassForbiddenError) Error() string {
	err := fmt.Sprintf("Device Class %s is forbidden for the current Tenant: ", i.deviceClassName)

	return utils.AllowedValuesErrorMessage(i.spec, err)
}

func NewDeviceClassForbidden(class string, spec api.SelectorAllowedListSpec) error {
	return &deviceClassForbiddenError{
		deviceClassName: class,
		spec:            spec,
	}
}

type deviceClassUndefinedError struct {
	spec api.SelectorAllowedListSpec
}

func NewDeviceClassUndefined(spec api.SelectorAllowedListSpec) error {
	return &deviceClassUndefinedError{
		spec: spec,
	}
}

func (i deviceClassUndefinedError) Error() string {
	return utils.AllowedValuesErrorMessage(i.spec, "Selected DeviceClass is forbidden for the current Tenant or does not exist. Specify a device Class which is allowed by ")
}
