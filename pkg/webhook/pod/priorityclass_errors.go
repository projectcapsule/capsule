// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package pod

import (
	"fmt"

	"github.com/clastix/capsule/pkg/api"
	"github.com/clastix/capsule/pkg/webhook/utils"
)

type podPriorityClassForbiddenError struct {
	priorityClassName string
	spec              api.SelectorAllowedListSpec
}

func NewPodPriorityClassForbidden(priorityClassName string, spec api.SelectorAllowedListSpec) error {
	return &podPriorityClassForbiddenError{
		priorityClassName: priorityClassName,
		spec:              spec,
	}
}

func (f podPriorityClassForbiddenError) Error() (err string) {
	err = fmt.Sprintf("Pod Priorioty Class %s is forbidden for the current Tenant: ", f.priorityClassName)

	return utils.AllowedValuesErrorMessage(f.spec, err)
}
