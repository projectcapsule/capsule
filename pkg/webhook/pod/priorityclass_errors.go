// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package pod

import (
	"fmt"
	"strings"

	capsulev1beta1 "github.com/clastix/capsule/api/v1beta1"
)

type podPriorityClassForbiddenError struct {
	priorityClassName string
	spec              capsulev1beta1.AllowedListSpec
}

func NewPodPriorityClassForbidden(priorityClassName string, spec capsulev1beta1.AllowedListSpec) error {
	return &podPriorityClassForbiddenError{
		priorityClassName: priorityClassName,
		spec:              spec,
	}
}

func (f podPriorityClassForbiddenError) Error() (err string) {
	err = fmt.Sprintf("Pod Priorioty Class %s is forbidden for the current Tenant: ", f.priorityClassName)

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
