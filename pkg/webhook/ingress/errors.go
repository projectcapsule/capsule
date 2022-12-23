// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package ingress

import (
	"fmt"
	"strings"

	"github.com/clastix/capsule/pkg/api"
)

type ingressClassForbiddenError struct {
	className string
	spec      api.SelectorAllowedListSpec
}

func NewIngressClassForbidden(className string, spec api.SelectorAllowedListSpec) error {
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

func NewIngressHostnamesNotValid(invalidHostnames []string, notMatchingHostnames []string, spec api.AllowedListSpec) error {
	return &ingressHostnameNotValidError{invalidHostnames: invalidHostnames, notMatchingHostnames: notMatchingHostnames, spec: spec}
}

func (i ingressHostnameNotValidError) Error() string {
	return fmt.Sprintf("Hostnames %s are not valid for the current Tenant. Hostnames %s not matching for the current Tenant%s",
		i.invalidHostnames, i.notMatchingHostnames, appendHostnameError(i.spec))
}

type ingressClassNotValidError struct {
	spec api.SelectorAllowedListSpec
}

func NewIngressClassNotValid(spec api.SelectorAllowedListSpec) error {
	return &ingressClassNotValidError{
		spec: spec,
	}
}

func (i ingressClassNotValidError) Error() string {
	return "A valid Ingress Class must be used" + appendClassError(i.spec)
}

// nolint:predeclared
func appendClassError(spec api.SelectorAllowedListSpec) (append string) {
	if len(spec.Exact) > 0 {
		append += fmt.Sprintf(", one of the following (%s)", strings.Join(spec.Exact, ", "))
	}

	if len(spec.Regex) > 0 {
		append += fmt.Sprintf(", or matching the regex %s", spec.Regex)
	}

	if len(spec.Selector.MatchLabels) > 0 || len(spec.Selector.MatchExpressions) > 0 {
		append += fmt.Sprintf(", or matching the label selector defined in the Tenant")
	}

	return
}

// nolint:predeclared
func appendHostnameError(spec api.AllowedListSpec) (append string) {
	if len(spec.Exact) > 0 {
		append = fmt.Sprintf(", specify one of the following (%s)", strings.Join(spec.Exact, ", "))
	}

	if len(spec.Regex) > 0 {
		append += fmt.Sprintf(", or matching the regex %s", spec.Regex)
	}

	return
}
