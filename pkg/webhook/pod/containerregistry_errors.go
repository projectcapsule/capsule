// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package pod

import (
	"fmt"
	"strings"

	capsulev1beta1 "github.com/clastix/capsule/api/v1beta1"
)

type missingContainerRegistryError struct {
	fqci string
}

func (m missingContainerRegistryError) Error() string {
	return fmt.Sprintf("container image %s is missing repository, please, use a fully qualified container image name", m.fqci)
}

func NewMissingContainerRegistryError(image string) error {
	return &missingContainerRegistryError{fqci: image}
}

type registryClassForbiddenError struct {
	fqci string
	spec capsulev1beta1.AllowedListSpec
}

func NewContainerRegistryForbidden(image string, spec capsulev1beta1.AllowedListSpec) error {
	return &registryClassForbiddenError{
		fqci: image,
		spec: spec,
	}
}

func (f registryClassForbiddenError) Error() (err string) {
	err = fmt.Sprintf("Container image %s registry is forbidden for the current Tenant: ", f.fqci)

	var extra []string

	if len(f.spec.Exact) > 0 {
		extra = append(extra, fmt.Sprintf("use one from the following list (%s)", strings.Join(f.spec.Exact, ", ")))
	}

	if len(f.spec.Regex) > 0 {
		extra = append(extra, fmt.Sprintf(" use one matching the following regex (%s)", f.spec.Regex))
	}

	err += strings.Join(extra, " or ")

	return
}
