// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package ingress

import (
	"fmt"
	"strings"

	"github.com/clastix/capsule/api/v1alpha1"
)

type ingressClassForbidden struct {
	className string
	spec      v1alpha1.AllowedListSpec
}

func NewIngressClassForbidden(className string, spec v1alpha1.AllowedListSpec) error {
	return &ingressClassForbidden{
		className: className,
		spec:      spec,
	}
}

func (i ingressClassForbidden) Error() string {
	return fmt.Sprintf("Ingress Class %s is forbidden for the current Tenant%s", i.className, appendClassError(i.spec))
}

type ingressHostnameNotValid struct {
	invalidHostnames     []string
	notMatchingHostnames []string
	spec                 v1alpha1.AllowedListSpec
}

type ingressHostnameCollision struct {
	hostname string
}

func (i ingressHostnameCollision) Error() string {
	return fmt.Sprintf("hostname %s is already used across the cluster: please, reach out to the system administrators", i.hostname)
}

func NewIngressHostnameCollision(hostname string) error {
	return ingressHostnameCollision{hostname: hostname}
}

func NewIngressHostnamesNotValid(invalidHostnames []string, notMatchingHostnames []string, spec v1alpha1.AllowedListSpec) error {
	return &ingressHostnameNotValid{invalidHostnames: invalidHostnames, notMatchingHostnames: notMatchingHostnames, spec: spec}
}

func (i ingressHostnameNotValid) Error() string {
	return fmt.Sprintf("Hostnames %s are not valid for the current Tenant. Hostnames %s not matching for the current Tenant%s",
		i.invalidHostnames, i.notMatchingHostnames, appendHostnameError(i.spec))
}

type ingressClassNotValid struct {
	spec v1alpha1.AllowedListSpec
}

func NewIngressClassNotValid(spec v1alpha1.AllowedListSpec) error {
	return &ingressClassNotValid{
		spec: spec,
	}
}

func (i ingressClassNotValid) Error() string {
	return "A valid Ingress Class must be used" + appendClassError(i.spec)
}

func appendClassError(spec v1alpha1.AllowedListSpec) (append string) {
	if len(spec.Exact) > 0 {
		append += fmt.Sprintf(", one of the following (%s)", strings.Join(spec.Exact, ", "))
	}
	if len(spec.Regex) > 0 {
		append += fmt.Sprintf(", or matching the regex %s", spec.Regex)
	}
	return
}

func appendHostnameError(spec v1alpha1.AllowedListSpec) (append string) {
	if len(spec.Exact) > 0 {
		append += fmt.Sprintf(", specify one of the following (%s)", strings.Join(spec.Exact, ", "))
	}
	if len(spec.Regex) > 0 {
		append += fmt.Sprintf(", or matching the regex %s", spec.Regex)
	}
	return
}
