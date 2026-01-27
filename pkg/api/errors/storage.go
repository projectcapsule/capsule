// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package errors

import (
	"fmt"

	"github.com/projectcapsule/capsule/internal/webhook/utils"
	"github.com/projectcapsule/capsule/pkg/api"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	evt "github.com/projectcapsule/capsule/pkg/runtime/events"
)

type StorageClassError struct {
	storageClass string
	msg          error
}

func NewStorageClassError(class string, msg error) error {
	return &StorageClassError{
		storageClass: class,
		msg:          msg,
	}
}

func (e StorageClassError) Error() string {
	return fmt.Sprintf("Failed to resolve Storage Class %s: %s", e.storageClass, e.msg)
}

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

type MissingPVTenantLabelsError struct {
	name   string
	action string
}

func (e *MissingPVTenantLabelsError) Reason() string { return evt.ReasonCrossTenantReference }
func (e *MissingPVTenantLabelsError) Action() string { return e.action }

func NewMissingTenantPVLabelsError(name string, action string) error {
	return &MissingPVTenantLabelsError{
		name:   name,
		action: action,
	}
}

func (e MissingPVTenantLabelsError) Error() string {
	return fmt.Sprintf("PersistentVolume %s is missing the Tenant label (%s), preventing a potential cross-tenant mount", e.name, meta.TenantLabel)
}

type CrossTenantPVMountError struct {
	name   string
	action string
}

func (e *CrossTenantPVMountError) Reason() string { return evt.ReasonCrossTenantReference }
func (e *CrossTenantPVMountError) Action() string { return e.action }

func NewCrossTenantPVMountError(name string, action string) error {
	return &CrossTenantPVMountError{
		name:   name,
		action: action,
	}
}

func (e CrossTenantPVMountError) Error() string {
	return fmt.Sprintf("Preventing a cross-tenant mount for PersistentVolume %s", e.name)
}

type PvSelectorError struct {
	action string
}

func (e *PvSelectorError) Reason() string { return evt.ReasonCrossTenantReference }
func (e *PvSelectorError) Action() string { return e.action }

func NewPVSelectorError(action string) error {
	return &PvSelectorError{
		action: action,
	}
}

func (m PvSelectorError) Error() string {
	return "PersistentVolume selectors are not allowed since unable to prevent cross-tenant mount"
}

type PvNotFoundError struct {
	name   string
	action string
}

func (e *PvNotFoundError) Reason() string { return evt.ReasonCrossTenantReference }
func (e *PvNotFoundError) Action() string { return e.action }

func NewPvNotFoundError(name string, action string) error {
	return &PvNotFoundError{
		name:   name,
		action: action,
	}
}

func (e PvNotFoundError) Error() string {
	return fmt.Sprintf("Cannot create a PVC referring to a not yet existing PersistentVolume %s", e.name)
}
