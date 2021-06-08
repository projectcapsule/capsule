// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package imagepullpolicy

import (
	"fmt"
	"strings"
)

type podPriorityClassForbidden struct {
	usedPullPolicy      string
	allowedPullPolicies []string
	containerName       string
}

func NewImagePullPolicyForbidden(usedPullPolicy, containerName string, allowedPullPolicies []string) error {
	return &podPriorityClassForbidden{
		usedPullPolicy:      usedPullPolicy,
		containerName:       containerName,
		allowedPullPolicies: allowedPullPolicies,
	}
}

func (f podPriorityClassForbidden) Error() (err string) {
	return fmt.Sprintf("the ImagePullPolicy %s for container %s is not allowed, use one of the followings: %s", f.usedPullPolicy, f.containerName, strings.Join(f.allowedPullPolicies, ", "))
}
