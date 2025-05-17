package gateway

import (
	"fmt"
	"github.com/projectcapsule/capsule/pkg/api"
	"github.com/projectcapsule/capsule/pkg/webhook/utils"
)

type gatewayClassForbiddenError struct {
	gatewayClassName string
	spec             api.DefaultAllowedListSpec
}

func NewGatewayClassForbidden(class string, spec api.DefaultAllowedListSpec) error {
	return &gatewayClassForbiddenError{
		gatewayClassName: class,
		spec:             spec,
	}
}

func (i gatewayClassForbiddenError) Error() string {
	err := fmt.Sprintf("Gateway Class %s is forbidden for the current Tenant: ", i.gatewayClassName)

	return utils.DefaultAllowedValuesErrorMessage(i.spec, err)
}

type gatewayClassUndefinedError struct {
	spec api.DefaultAllowedListSpec
}

func NewGatewayClassUndefined(spec api.DefaultAllowedListSpec) error {
	return &gatewayClassUndefinedError{
		spec: spec,
	}
}

func (i gatewayClassUndefinedError) Error() string {
	return utils.DefaultAllowedValuesErrorMessage(i.spec, "No gateway Class is forbidden for the current Tenant. Specify a gateway Class which is allowed within the Tenant: ")
}
