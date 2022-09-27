// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package service

import (
	"fmt"
	"strings"

	"github.com/clastix/capsule/pkg/api"
)

type externalServiceIPForbiddenError struct {
	cidr []string
}

func NewExternalServiceIPForbidden(allowedIps []api.AllowedIP) error {
	cidr := make([]string, 0, len(allowedIps))

	for _, i := range allowedIps {
		cidr = append(cidr, string(i))
	}

	return &externalServiceIPForbiddenError{
		cidr: cidr,
	}
}

func (e externalServiceIPForbiddenError) Error() string {
	if len(e.cidr) == 0 {
		return "The current Tenant does not allow the use of Service with external IPs"
	}

	return fmt.Sprintf("The selected external IPs for the current Service are violating the following enforced CIDRs: %s", strings.Join(e.cidr, ", "))
}

type nodePortDisabledError struct{}

func NewNodePortDisabledError() error {
	return &nodePortDisabledError{}
}

func (nodePortDisabledError) Error() string {
	return "NodePort service types are forbidden for the tenant: please, reach out to the system administrators"
}

type externalNameDisabledError struct{}

func NewExternalNameDisabledError() error {
	return &externalNameDisabledError{}
}

func (externalNameDisabledError) Error() string {
	return "ExternalName service types are forbidden for the tenant: please, reach out to the system administrators"
}

type loadBalancerDisabledError struct{}

func NewLoadBalancerDisabled() error {
	return &loadBalancerDisabledError{}
}

func (loadBalancerDisabledError) Error() string {
	return "LoadBalancer service types are forbidden for the tenant: please, reach out to the system administrators"
}
