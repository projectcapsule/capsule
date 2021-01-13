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

package pvc

import (
	"fmt"
	"strings"

	"github.com/clastix/capsule/api/v1alpha1"
)

type storageClassNotValid struct {
	spec v1alpha1.AllowedListSpec
}

func NewStorageClassNotValid(storageClasses v1alpha1.AllowedListSpec) error {
	return &storageClassNotValid{
		spec: storageClasses,
	}
}

func appendError(spec v1alpha1.AllowedListSpec) (append string) {
	if len(spec.Exact) > 0 {
		append += fmt.Sprintf(", one of the following (%s)", strings.Join(spec.Exact, ", "))
	}
	if len(spec.Regex) > 0 {
		append += fmt.Sprintf(", or matching the regex %s", spec.Regex)
	}
	return
}

func (s storageClassNotValid) Error() (err string) {
	return "A valid Storage Class must be used" + appendError(s.spec)
}

type storageClassForbidden struct {
	className string
	spec      v1alpha1.AllowedListSpec
}

func NewStorageClassForbidden(className string, storageClasses v1alpha1.AllowedListSpec) error {
	return &storageClassForbidden{
		className: className,
		spec:      storageClasses,
	}
}

func (f storageClassForbidden) Error() string {
	return fmt.Sprintf("Storage Class %s is forbidden for the current Tenant%s", f.className, appendError(f.spec))
}
