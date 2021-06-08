// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package domain

import (
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	podAllowedImagePullPolicyAnnotation = "capsule.clastix.io/allowed-image-pull-policy"
)

type ImagePullPolicy interface {
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

func NewImagePullPolicy(object metav1.Object) ImagePullPolicy {
	annotations := object.GetAnnotations()

	v, ok := annotations[podAllowedImagePullPolicyAnnotation]
	// the Tenant doesn't enforce the allowed image pull policy, returning nil
	if !ok {
		return nil
	}

	return &imagePullPolicyValidator{
		allowedPolicies: strings.Split(v, ","),
	}
}
