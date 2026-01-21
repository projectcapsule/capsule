// Copyright 2020-2025 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0
package errors

import (
	"fmt"

	"github.com/projectcapsule/capsule/internal/webhook/utils"
	"github.com/projectcapsule/capsule/pkg/api"
)

type DeviceClassForbiddenError struct {
	deviceClassName string
	spec            api.SelectorAllowedListSpec
}

func (i DeviceClassForbiddenError) Error() string {
	err := fmt.Sprintf("Device Class %s is forbidden for the current Tenant: ", i.deviceClassName)

	return utils.AllowedValuesErrorMessage(i.spec, err)
}

func NewDeviceClassForbidden(class string, spec api.SelectorAllowedListSpec) error {
	return &DeviceClassForbiddenError{
		deviceClassName: class,
		spec:            spec,
	}
}

type DeviceClassUndefinedError struct {
	spec api.SelectorAllowedListSpec
}

func NewDeviceClassUndefined(spec api.SelectorAllowedListSpec) error {
	return &DeviceClassUndefinedError{
		spec: spec,
	}
}

func (i DeviceClassUndefinedError) Error() string {
	return utils.AllowedValuesErrorMessage(i.spec, "Selected DeviceClass is forbidden for the current Tenant or does not exist. Specify a device Class which is allowed by ")
}

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

type IngressClassError struct {
	ingressClass string
	msg          error
}

func NewIngressClassError(class string, msg error) error {
	return &IngressClassError{
		ingressClass: class,
		msg:          msg,
	}
}

func (e IngressClassError) Error() string {
	return fmt.Sprintf("Failed to resolve Ingress Class %s: %s", e.ingressClass, e.msg)
}

type IngressClassForbiddenError struct {
	ingressClassName string
	spec             api.DefaultAllowedListSpec
}

func NewIngressClassForbidden(class string, spec api.DefaultAllowedListSpec) error {
	return &IngressClassForbiddenError{
		ingressClassName: class,
		spec:             spec,
	}
}

func (i IngressClassForbiddenError) Error() string {
	err := fmt.Sprintf("Ingress Class %s is forbidden for the current Tenant: ", i.ingressClassName)

	return utils.DefaultAllowedValuesErrorMessage(i.spec, err)
}

type GatewayClassError struct {
	gatewayClass string
	msg          error
}

func NewGatewayClassError(class string, msg error) error {
	return &GatewayClassError{
		gatewayClass: class,
		msg:          msg,
	}
}

func (e GatewayClassError) Error() string {
	return fmt.Sprintf("Failed to resolve Gateway Class %s: %s", e.gatewayClass, e.msg)
}

type PriorityClassError struct {
	priorityClass string
	msg           error
}

func NewPriorityClassError(class string, msg error) error {
	return &PriorityClassError{
		priorityClass: class,
		msg:           msg,
	}
}

func (e PriorityClassError) Error() string {
	return fmt.Sprintf("Failed to resolve Priority Class %s: %s", e.priorityClass, e.msg)
}
