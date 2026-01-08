// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package pod

import (
	"fmt"

	"github.com/projectcapsule/capsule/internal/webhook/utils"
	"github.com/projectcapsule/capsule/pkg/api"
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
