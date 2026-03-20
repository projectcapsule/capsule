// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package errors

import (
	"fmt"
	"reflect"
	"strings"

	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	"github.com/projectcapsule/capsule/internal/webhook/utils"
	"github.com/projectcapsule/capsule/pkg/api"
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

// GatewayForbiddenError is returned when an HTTPRoute references a Gateway that
// is not in the allowed list for the Tenant's namespace rules.
type GatewayForbiddenError struct {
	gatewayNamespace string
	gatewayName      string
	spec             api.AllowedGatewaySpec
}

func NewGatewayForbidden(name, namespace string, spec api.AllowedGatewaySpec) error {
	return &GatewayForbiddenError{
		gatewayNamespace: namespace,
		gatewayName:      name,
		spec:             spec,
	}
}

func (e GatewayForbiddenError) Error() string {
	msg := fmt.Sprintf("Gateway %s/%s is forbidden for the current Tenant: ", e.gatewayNamespace, e.gatewayName)

	parts := []string{msg}

	if e.spec.Default != nil {
		parts = append(parts, fmt.Sprintf("default: %s/%s", e.spec.Default.Namespace, e.spec.Default.Name))
	}

	if len(e.spec.Allowed) > 0 {
		names := make([]string, 0, len(e.spec.Allowed))

		for _, a := range e.spec.Allowed {
			names = append(names, fmt.Sprintf("%s/%s", a.Namespace, a.Name))
		}

		parts = append(parts, fmt.Sprintf("allowed: [%s]", strings.Join(names, ", ")))
	}

	return strings.Join(parts, " ")
}
