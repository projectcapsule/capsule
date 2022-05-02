// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package pod

import (
	"fmt"
	"strings"
)

type imagePullPolicyForbiddenError struct {
	usedPullPolicy      string
	allowedPullPolicies []string
	containerName       string
}

func NewImagePullPolicyForbidden(usedPullPolicy, containerName string, allowedPullPolicies []string) error {
	return &imagePullPolicyForbiddenError{
		usedPullPolicy:      usedPullPolicy,
		containerName:       containerName,
		allowedPullPolicies: allowedPullPolicies,
	}
}

func (f imagePullPolicyForbiddenError) Error() (err string) {
	return fmt.Sprintf("ImagePullPolicy %s for container %s is forbidden, use one of the followings: %s", f.usedPullPolicy, f.containerName, strings.Join(f.allowedPullPolicies, ", "))
}
