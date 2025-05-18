// Copyright 2020-2023 Project Capsule Authors.
// SPDX-License-Identifier: Apache-2.0

package gateway

import (
	"fmt"

	"github.com/projectcapsule/capsule/pkg/api"
	"github.com/projectcapsule/capsule/pkg/webhook/utils"
)

type gatewayClassForbiddenError struct {
	gatewayClassName string
	spec             api.SelectionListWithDefaultSpec
}

func NewGatewayClassForbidden(class string, spec api.SelectionListWithDefaultSpec) error {
	return &gatewayClassForbiddenError{
		gatewayClassName: class,
		spec:             spec,
	}
}

func (i gatewayClassForbiddenError) Error() string {
	err := fmt.Sprintf("Gateway Class %s is forbidden for the current Tenant: ", i.gatewayClassName)

	return utils.SelectionListWithDefaultErrorMessage(i.spec, err)
}

type gatewayClassUndefinedError struct {
	spec api.SelectionListWithDefaultSpec
}

func NewGatewayClassUndefined(spec api.SelectionListWithDefaultSpec) error {
	return &gatewayClassUndefinedError{
		spec: spec,
	}
}

func (i gatewayClassUndefinedError) Error() string {
	return utils.SelectionListWithDefaultErrorMessage(i.spec, "No gateway Class is forbidden for the current Tenant. Specify a gateway Class which is allowed within the Tenant: ")
}
