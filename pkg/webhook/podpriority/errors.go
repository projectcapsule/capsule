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
