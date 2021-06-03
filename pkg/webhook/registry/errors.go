// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package registry

import (
	"fmt"
	"strings"

	"github.com/clastix/capsule/api/v1alpha1"
)

type registryClassForbidden struct {
	fqdi string
	spec v1alpha1.AllowedListSpec
}

func NewContainerRegistryForbidden(image string, spec v1alpha1.AllowedListSpec) error {
	return &registryClassForbidden{
		fqdi: image,
		spec: spec,
	}
}

func (f registryClassForbidden) Error() (err string) {
	err = fmt.Sprintf("Container image %s registry is forbidden for the current Tenant: ", f.fqdi)
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
