// Copyright 2020-2025 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package pod

import (
	"fmt"

	"github.com/projectcapsule/capsule/pkg/api"
	"github.com/projectcapsule/capsule/pkg/webhook/utils"
)

type podPriorityClassForbiddenError struct {
	priorityClassName string
	spec              api.DefaultAllowedListSpec
}

func NewPodPriorityClassForbidden(priorityClassName string, spec api.DefaultAllowedListSpec) error {
	return &podPriorityClassForbiddenError{
		priorityClassName: priorityClassName,
		spec:              spec,
	}
}

func (f podPriorityClassForbiddenError) Error() (err string) {
	msg := fmt.Sprintf("Pod Priority Class %s is forbidden for the current Tenant: ", f.priorityClassName)

	return utils.DefaultAllowedValuesErrorMessage(f.spec, msg)
}
