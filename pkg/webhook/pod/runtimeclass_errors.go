// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package pod

import (
	"fmt"

	"github.com/clastix/capsule/pkg/api"
	"github.com/clastix/capsule/pkg/webhook/utils"
)

type podRuntimeClassForbiddenError struct {
	runtimeClassName string
	spec             api.SelectorAllowedListSpec
}

func NewPodRuntimeClassForbidden(runtimeClassName string, spec api.SelectorAllowedListSpec) error {
	return &podRuntimeClassForbiddenError{
		runtimeClassName: runtimeClassName,
		spec:             spec,
	}
}

func (f podRuntimeClassForbiddenError) Error() (err string) {
	err = fmt.Sprintf("Pod Runtime Class %s is forbidden for the current Tenant: ", f.runtimeClassName)

	return utils.AllowedValuesErrorMessage(f.spec, err)
}
