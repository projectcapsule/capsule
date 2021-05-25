/*
Copyright 2020 Clastix Labs.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

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
