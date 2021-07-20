// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package service

import (
	"fmt"
	"strings"

	capsulev1beta1 "github.com/clastix/capsule/api/v1beta1"
)

type externalServiceIPForbidden struct {
	cidr []string
}

func NewExternalServiceIPForbidden(allowedIps []capsulev1beta1.AllowedIP) error {
	var cidr []string
	for _, i := range allowedIps {
		cidr = append(cidr, string(i))
	}
	return &externalServiceIPForbidden{
		cidr: cidr,
	}
}

func (e externalServiceIPForbidden) Error() string {
	if len(e.cidr) == 0 {
		return "The current Tenant does not allow the use of Service with external IPs"
	}

	return fmt.Sprintf("The selected external IPs for the current Service are violating the following enforced CIDRs: %s", strings.Join(e.cidr, ", "))
}

type nodePortDisabled struct{}

func NewNodePortDisabledError() error {
	return &nodePortDisabled{}
}

func (nodePortDisabled) Error() string {
	return "NodePort service types are forbidden for the tenant: please, reach out to the system administrators"
}

type externalNameDisabled struct{}

func NewExternalNameDisabledError() error {
	return &externalNameDisabled{}
}

func (externalNameDisabled) Error() string {
	return "ExternalName service types are forbidden for the tenant: please, reach out to the system administrators"
}
