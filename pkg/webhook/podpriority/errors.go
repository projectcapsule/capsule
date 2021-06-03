// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package podpriority

import (
	"fmt"
	"strings"

	"github.com/clastix/capsule/api/v1alpha1"
)

type podPriorityClassForbidden struct {
	priorityClassName string
	spec              v1alpha1.AllowedListSpec
}

func NewPodPriorityClassForbidden(priorityClassName string, spec v1alpha1.AllowedListSpec) error {
	return &podPriorityClassForbidden{
		priorityClassName: priorityClassName,
		spec:              spec,
	}
}

func (f podPriorityClassForbidden) Error() (err string) {
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
