// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package errors

import (
	"fmt"
	"strings"

	"github.com/projectcapsule/capsule/internal/webhook/utils"
	"github.com/projectcapsule/capsule/pkg/api"
)

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

type IngressHostnameNotValidError struct {
	invalidHostnames     []string
	notMatchingHostnames []string
	spec                 api.AllowedListSpec
}

type IngressHostnameCollisionError struct {
	hostname string
}

func (i IngressHostnameCollisionError) Error() string {
	return fmt.Sprintf("hostname %s is already used across the cluster: please, reach out to the system administrators", i.hostname)
}

func NewIngressHostnameCollision(hostname string) error {
	return &IngressHostnameCollisionError{hostname: hostname}
}

func NewEmptyIngressHostname(spec api.AllowedListSpec) error {
	return &EmptyIngressHostnameError{
		spec: spec,
	}
}

type EmptyIngressHostnameError struct {
	spec api.AllowedListSpec
}

func (e EmptyIngressHostnameError) Error() string {
	return fmt.Sprintf("empty hostname is not allowed for the current Tenant%s", appendHostnameError(e.spec))
}

func NewIngressHostnamesNotValid(invalidHostnames []string, notMatchingHostnames []string, spec api.AllowedListSpec) error {
	return &IngressHostnameNotValidError{invalidHostnames: invalidHostnames, notMatchingHostnames: notMatchingHostnames, spec: spec}
}

func (i IngressHostnameNotValidError) Error() string {
	return fmt.Sprintf("Hostnames %s are not valid for the current Tenant. Hostnames %s not matching for the current Tenant%s",
		i.invalidHostnames, i.notMatchingHostnames, appendHostnameError(i.spec))
}

type IngressClassUndefinedError struct {
	spec api.DefaultAllowedListSpec
}

func NewIngressClassUndefined(spec api.DefaultAllowedListSpec) error {
	return &IngressClassUndefinedError{
		spec: spec,
	}
}

func (i IngressClassUndefinedError) Error() string {
	return utils.DefaultAllowedValuesErrorMessage(i.spec, "No Ingress Class is forbidden for the current Tenant. Specify a Ingress Class which is allowed within the Tenant: ")
}

type IngressClassNotValidError struct {
	ingressClassName string
	spec             api.DefaultAllowedListSpec
}

func NewIngressClassNotValid(class string, spec api.DefaultAllowedListSpec) error {
	return &IngressClassNotValidError{
		ingressClassName: class,
		spec:             spec,
	}
}

func (i IngressClassNotValidError) Error() string {
	err := fmt.Sprintf("Ingress Class %s is forbidden for the current Tenant: ", i.ingressClassName)

	return utils.DefaultAllowedValuesErrorMessage(i.spec, err)
}

//nolint:predeclared,revive
func appendHostnameError(spec api.AllowedListSpec) (append string) {
	if len(spec.Exact) > 0 {
		append = fmt.Sprintf(", specify one of the following (%s)", strings.Join(spec.Exact, ", "))
	}

	//nolint:staticcheck
	if len(spec.Regex) > 0 {
		append += fmt.Sprintf(", or matching the regex %s", spec.Regex)
	}

	return append
}
