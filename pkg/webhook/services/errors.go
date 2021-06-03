// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package services

import (
	"fmt"
	"strings"

	"github.com/clastix/capsule/api/v1alpha1"
)

type externalServiceIPForbidden struct {
	cidr []string
}

func NewExternalServiceIPForbidden(allowedIps []v1alpha1.AllowedIP) error {
	var cidr []string
	for _, i := range allowedIps {
		cidr = append(cidr, string(i))
	}
	return &externalServiceIPForbidden{
		cidr: cidr,
	}
}

func (e externalServiceIPForbidden) Error() string {
	return fmt.Sprintf("The selected external IPs for the current Service are violating the following enforced CIDRs: %s", strings.Join(e.cidr, ", "))
}

type nodePortDisabled struct{}

func NewNodePortDisabledError() error {
	return &nodePortDisabled{}
}

func (nodePortDisabled) Error() string {
	return "NodePort service types are disabled for a tenant: please, reach out to the system administrators"
}
