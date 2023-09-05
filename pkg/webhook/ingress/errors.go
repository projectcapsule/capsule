// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package ingress

import (
	"fmt"
	"strings"

	"github.com/clastix/capsule/pkg/api"
	"github.com/clastix/capsule/pkg/webhook/utils"
)

type ingressClassForbiddenError struct {
	ingressClassName string
	spec             api.DefaultAllowedListSpec
}

func NewIngressClassForbidden(class string, spec api.DefaultAllowedListSpec) error {
	return &ingressClassForbiddenError{
		ingressClassName: class,
		spec:             spec,
	}
}

func (i ingressClassForbiddenError) Error() string {
	err := fmt.Sprintf("Ingress Class %s is forbidden for the current Tenant: ", i.ingressClassName)

	return utils.DefaultAllowedValuesErrorMessage(i.spec, err)
}

type ingressHostnameNotValidError struct {
	invalidHostnames     []string
	notMatchingHostnames []string
	spec                 api.AllowedListSpec
}

type ingressHostnameCollisionError struct {
	hostname string
}

func (i ingressHostnameCollisionError) Error() string {
	return fmt.Sprintf("hostname %s is already used across the cluster: please, reach out to the system administrators", i.hostname)
}

func NewIngressHostnameCollision(hostname string) error {
	return &ingressHostnameCollisionError{hostname: hostname}
}

func NewEmptyIngressHostname(spec api.AllowedListSpec) error {
	return &emptyIngressHostnameError{
		spec: spec,
	}
}

type emptyIngressHostnameError struct {
	spec api.AllowedListSpec
}

func (e emptyIngressHostnameError) Error() string {
	return fmt.Sprintf("empty hostname is not allowed for the current Tenant%s", appendHostnameError(e.spec))
}

func NewIngressHostnamesNotValid(invalidHostnames []string, notMatchingHostnames []string, spec api.AllowedListSpec) error {
	return &ingressHostnameNotValidError{invalidHostnames: invalidHostnames, notMatchingHostnames: notMatchingHostnames, spec: spec}
}

func (i ingressHostnameNotValidError) Error() string {
	return fmt.Sprintf("Hostnames %s are not valid for the current Tenant. Hostnames %s not matching for the current Tenant%s",
		i.invalidHostnames, i.notMatchingHostnames, appendHostnameError(i.spec))
}

type ingressClassUndefinedError struct {
	spec api.DefaultAllowedListSpec
}

func NewIngressClassUndefined(spec api.DefaultAllowedListSpec) error {
	return &ingressClassUndefinedError{
		spec: spec,
	}
}

func (i ingressClassUndefinedError) Error() string {
	return utils.DefaultAllowedValuesErrorMessage(i.spec, "No Ingress Class is forbidden for the current Tenant. Specify a Ingress Class which is allowed within the Tenant: ")
}

type ingressClassNotValidError struct {
	ingressClassName string
	spec             api.DefaultAllowedListSpec
}

func NewIngressClassNotValid(class string, spec api.DefaultAllowedListSpec) error {
	return &ingressClassNotValidError{
		ingressClassName: class,
		spec:             spec,
	}
}

func (i ingressClassNotValidError) Error() string {
	err := fmt.Sprintf("Ingress Class %s is forbidden for the current Tenant: ", i.ingressClassName)

	return utils.DefaultAllowedValuesErrorMessage(i.spec, err)
}

//nolint:predeclared
func appendHostnameError(spec api.AllowedListSpec) (append string) {
	if len(spec.Exact) > 0 {
		append = fmt.Sprintf(", specify one of the following (%s)", strings.Join(spec.Exact, ", "))
	}

	if len(spec.Regex) > 0 {
		append += fmt.Sprintf(", or matching the regex %s", spec.Regex)
	}

	return
}
