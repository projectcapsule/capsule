// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package ingress

import (
	"fmt"
	"strings"

	capsulev1beta1 "github.com/clastix/capsule/api/v1beta1"
)

type ingressClassForbiddenError struct {
	className string
	spec      capsulev1beta1.AllowedListSpec
}

func NewIngressClassForbidden(className string, spec capsulev1beta1.AllowedListSpec) error {
	return &ingressClassForbiddenError{
		className: className,
		spec:      spec,
	}
}

func (i ingressClassForbiddenError) Error() string {
	return fmt.Sprintf("Ingress Class %s is forbidden for the current Tenant%s", i.className, appendClassError(i.spec))
}

type ingressHostnameNotValidError struct {
	invalidHostnames     []string
	notMatchingHostnames []string
	spec                 capsulev1beta1.AllowedListSpec
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

func NewIngressHostnamesNotValid(invalidHostnames []string, notMatchingHostnames []string, spec capsulev1beta1.AllowedListSpec) error {
	return &ingressHostnameNotValidError{invalidHostnames: invalidHostnames, notMatchingHostnames: notMatchingHostnames, spec: spec}
}

func (i ingressHostnameNotValidError) Error() string {
	return fmt.Sprintf("Hostnames %s are not valid for the current Tenant. Hostnames %s not matching for the current Tenant%s",
		i.invalidHostnames, i.notMatchingHostnames, appendHostnameError(i.spec))
}

type ingressClassNotValidError struct {
	spec capsulev1beta1.AllowedListSpec
}

func NewIngressClassNotValid(spec capsulev1beta1.AllowedListSpec) error {
	return &ingressClassNotValidError{
		spec: spec,
	}
}

func (i ingressClassNotValidError) Error() string {
	return "A valid Ingress Class must be used" + appendClassError(i.spec)
}

// nolint:predeclared
func appendClassError(spec capsulev1beta1.AllowedListSpec) (append string) {
	if len(spec.Exact) > 0 {
		append += fmt.Sprintf(", one of the following (%s)", strings.Join(spec.Exact, ", "))
	}

	if len(spec.Regex) > 0 {
		append += fmt.Sprintf(", or matching the regex %s", spec.Regex)
	}

	return
}

// nolint:predeclared
func appendHostnameError(spec capsulev1beta1.AllowedListSpec) (append string) {
	if len(spec.Exact) > 0 {
		append = fmt.Sprintf(", specify one of the following (%s)", strings.Join(spec.Exact, ", "))
	}

	if len(spec.Regex) > 0 {
		append += fmt.Sprintf(", or matching the regex %s", spec.Regex)
	}

	return
}
