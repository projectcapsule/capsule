// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package errors

import (
	"fmt"
	"strings"

	"github.com/projectcapsule/capsule/pkg/api"
)

type NoServicesMetadataError struct {
	objectName string
}

func NewNoServicesMetadata(objectName string) error {
	return &NoServicesMetadataError{objectName: objectName}
}

func (n NoServicesMetadataError) Error() string {
	return fmt.Sprintf("Skipping labels sync for %s because no AdditionalLabels or AdditionalAnnotations presents in Tenant spec", n.objectName)
}

type ExternalServiceIPForbiddenError struct {
	cidr []string
}

func NewExternalServiceIPForbidden(allowedIps []api.AllowedIP) error {
	cidr := make([]string, 0, len(allowedIps))

	for _, i := range allowedIps {
		cidr = append(cidr, string(i))
	}

	return &ExternalServiceIPForbiddenError{
		cidr: cidr,
	}
}

func (e ExternalServiceIPForbiddenError) Error() string {
	if len(e.cidr) == 0 {
		return "external IPs are not allowed by the tenant"
	}

	return fmt.Sprintf("external IPs must use an allowed CIDR. Allowed CIDRs: %s", strings.Join(e.cidr, ", "))
}

type NodePortDisabledError struct{}

func NewNodePortDisabledError() error {
	return &NodePortDisabledError{}
}

func (NodePortDisabledError) Error() string {
	return "NodePort service types are forbidden for the tenant"
}

type ExternalNameDisabledError struct{}

func NewExternalNameDisabledError() error {
	return &ExternalNameDisabledError{}
}

func (ExternalNameDisabledError) Error() string {
	return "ExternalName service types are forbidden for the tenant"
}

type LoadBalancerDisabledError struct{}

func NewLoadBalancerDisabled() error {
	return &LoadBalancerDisabledError{}
}

func (LoadBalancerDisabledError) Error() string {
	return "LoadBalancer service types are forbidden for the tenant"
}
