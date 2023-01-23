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

type missingPVLabelsError struct {
	name string
}

func NewMissingPVLabelsError(name string) error {
	return &missingPVLabelsError{name: name}
}

func (m missingPVLabelsError) Error() string {
	return fmt.Sprintf("PeristentVolume %s is missing any label, please, ask the Cluster Administrator to label it", m.name)
}

type missingPVTenantLabelsError struct {
	name string
}

func NewMissingTenantPVLabelsError(name string) error {
	return &missingPVTenantLabelsError{name: name}
}

func (m missingPVTenantLabelsError) Error() string {
	return fmt.Sprintf("PeristentVolume %s is missing the Capsule Tenant label, preventing a potential cross-tenant mount", m.name)
}

type crossTenantPVMountError struct {
	name string
}

func NewCrossTenantPVMountError(name string) error {
	return &crossTenantPVMountError{
		name: name,
	}
}

func (m crossTenantPVMountError) Error() string {
	return fmt.Sprintf("PeristentVolume %s cannot be used by the following Tenant, preventing a cross-tenant mount", m.name)
}

type pvSelectorError struct{}

func NewPVSelectorError() error {
	return &pvSelectorError{}
}

func (m pvSelectorError) Error() string {
	return "PersistentVolume selectors are not allowed since unable to prevent cross-tenant mount"
}
