// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package pod

import (
	"strings"

	capsulev1beta2 "github.com/clastix/capsule/api/v1beta2"
)

type PullPolicy interface {
	IsPolicySupported(policy string) bool
	AllowedPullPolicies() []string
}

type imagePullPolicyValidator struct {
	allowedPolicies []string
}

func (i imagePullPolicyValidator) IsPolicySupported(policy string) bool {
	for _, allowed := range i.allowedPolicies {
		if strings.EqualFold(allowed, policy) {
			return true
		}
	}

	return false
}

func (i imagePullPolicyValidator) AllowedPullPolicies() []string {
	return i.allowedPolicies
}

func NewPullPolicy(tenant *capsulev1beta2.Tenant) PullPolicy {
	// the Tenant doesn't enforce the allowed image pull policy, returning nil
	if len(tenant.Spec.ImagePullPolicies) == 0 {
		return nil
	}

	allowedPolicies := make([]string, 0, len(tenant.Spec.ImagePullPolicies))

	for _, policy := range tenant.Spec.ImagePullPolicies {
		allowedPolicies = append(allowedPolicies, policy.String())
	}

	return &imagePullPolicyValidator{
		allowedPolicies: allowedPolicies,
	}
}
