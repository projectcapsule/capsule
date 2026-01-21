// Copyright 2020-2025 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package errors

import (
	"fmt"

	"github.com/projectcapsule/capsule/internal/webhook/utils"
	"github.com/projectcapsule/capsule/pkg/api"
)

type StorageClassNotValidError struct {
	spec api.DefaultAllowedListSpec
}

func NewStorageClassNotValid(storageClasses api.DefaultAllowedListSpec) error {
	return &StorageClassNotValidError{
		spec: storageClasses,
	}
}

func (s StorageClassNotValidError) Error() (err string) {
	msg := "A valid Storage Class must be used: "

	return utils.DefaultAllowedValuesErrorMessage(s.spec, msg)
}

type StorageClassForbiddenError struct {
	className string
	spec      api.DefaultAllowedListSpec
}

func NewStorageClassForbidden(className string, storageClasses api.DefaultAllowedListSpec) error {
	return &StorageClassForbiddenError{
		className: className,
		spec:      storageClasses,
	}
}

func (f StorageClassForbiddenError) Error() string {
	msg := fmt.Sprintf("Storage Class %s is forbidden for the current Tenant ", f.className)

	return utils.DefaultAllowedValuesErrorMessage(f.spec, msg)
}

type MissingPVLabelsError struct {
	name string
}

func NewMissingPVLabelsError(name string) error {
	return &MissingPVLabelsError{name: name}
}

func (m MissingPVLabelsError) Error() string {
	return fmt.Sprintf("PersistentVolume %s is missing any label, please, ask the Cluster Administrator to label it", m.name)
}

type MissingPVTenantLabelsError struct {
	name string
}

func NewMissingTenantPVLabelsError(name string) error {
	return &MissingPVTenantLabelsError{name: name}
}

func (m MissingPVTenantLabelsError) Error() string {
	return fmt.Sprintf("PersistentVolume %s is missing the Capsule Tenant label, preventing a potential cross-tenant mount", m.name)
}

type CrossTenantPVMountError struct {
	name string
}

func NewCrossTenantPVMountError(name string) error {
	return &CrossTenantPVMountError{
		name: name,
	}
}

func (m CrossTenantPVMountError) Error() string {
	return fmt.Sprintf("PersistentVolume %s cannot be used by the following Tenant, preventing a cross-tenant mount", m.name)
}

type PvSelectorError struct{}

func NewPVSelectorError() error {
	return &PvSelectorError{}
}

func (m PvSelectorError) Error() string {
	return "PersistentVolume selectors are not allowed since unable to prevent cross-tenant mount"
}
