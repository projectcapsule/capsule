/*
Copyright 2020 Clastix Labs.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

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
	spec                 v1alpha1.IngressHostnamesSpec
}

func NewIngressHostnamesNotValid(invalidHostnames []string, notMatchingHostnames []string, spec v1alpha1.IngressHostnamesSpec) error {
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

func appendHostnameError(spec v1alpha1.IngressHostnamesSpec) (append string) {
	if len(spec.Allowed) > 0 {
		append += fmt.Sprintf(", specify one of the following (%s)", strings.Join(spec.Allowed, ", "))
	}
	if len(spec.AllowedRegex) > 0 {
		append += fmt.Sprintf(", or matching the regex %s", spec.AllowedRegex)
	}
	return
}
