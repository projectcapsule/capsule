// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package domain

import (
	"regexp"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/clastix/capsule/api/v1alpha1"
)

const (
	podPriorityAllowedAnnotation      = "priorityclass.capsule.clastix.io/allowed"
	podPriorityAllowedRegexAnnotation = "priorityclass.capsule.clastix.io/allowed-regex"
)

func NewPodPriority(object metav1.Object) (allowed *v1alpha1.AllowedListSpec) {
	annotations := object.GetAnnotations()

	if v, ok := annotations[podPriorityAllowedAnnotation]; ok {
		allowed = &v1alpha1.AllowedListSpec{}
		allowed.Exact = strings.Split(v, ",")
	}

	if v, ok := annotations[podPriorityAllowedRegexAnnotation]; ok {
		if _, err := regexp.Compile(v); err == nil {
			if allowed == nil {
				allowed = &v1alpha1.AllowedListSpec{}
			}
			allowed.Regex = v
		}
	}

	return
}
