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

package registry

import (
	"fmt"
)

type registryClassNotValid struct{}

func NewRegistryClassNotValid() error {
	return &registryClassNotValid{}
}

func (registryClassNotValid) Error() string {
	return "A valid Registry Class must be used"
}

type registryClassForbidden struct {
	registryClassName string
}

func NewregistryClassForbidden(registryClassName string) error {
	return &registryClassForbidden{registryClassName: registryClassName}
}

func (f registryClassForbidden) Error() string {
	return fmt.Sprintf("Registry Class %s is forbidden for the current Tenant", f.registryClassName)
}
