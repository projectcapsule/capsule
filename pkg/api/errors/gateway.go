// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package errors

import (
	"fmt"
	"reflect"

	"github.com/projectcapsule/capsule/internal/webhook/utils"
	"github.com/projectcapsule/capsule/pkg/api"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
)

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

type GatewayError struct {
	gateway string
	msg     error
}

func NewGatewayError(gateway gatewayv1.ObjectName, msg error) error {
	return &GatewayError{
		gateway: reflect.ValueOf(gateway).String(),
		msg:     msg,
	}
}

func (e GatewayError) Error() string {
	return fmt.Sprintf("Failed to resolve Gateway %s: %s", e.gateway, e.msg)
}

type GatewayClassForbiddenError struct {
	gatewayClassName string
	spec             api.DefaultAllowedListSpec
}

func NewGatewayClassForbidden(class string, spec api.DefaultAllowedListSpec) error {
	return &GatewayClassForbiddenError{
		gatewayClassName: class,
		spec:             spec,
	}
}

func (i GatewayClassForbiddenError) Error() string {
	err := fmt.Sprintf("Gateway Class %s is forbidden for the current Tenant: ", i.gatewayClassName)

	return utils.DefaultAllowedValuesErrorMessage(i.spec, err)
}

type GatewayClassUndefinedError struct {
	spec api.DefaultAllowedListSpec
}

func NewGatewayClassUndefined(spec api.DefaultAllowedListSpec) error {
	return &GatewayClassUndefinedError{
		spec: spec,
	}
}

func (i GatewayClassUndefinedError) Error() string {
	return utils.DefaultAllowedValuesErrorMessage(i.spec, "No gateway Class is forbidden for the current Tenant. Specify a gateway Class which is allowed within the Tenant: ")
}
