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
)

type storageClassNotValid struct{}

func NewStorageClassNotValid() error {
	return &storageClassNotValid{}
}

func (storageClassNotValid) Error() string {
	return "A valid Storage Class must be used"
}

type storageClassForbidden struct {
	storageClassName string
}

func NewStorageClassForbidden(storageClassName string) error {
	return &storageClassForbidden{storageClassName: storageClassName}
}

func (f storageClassForbidden) Error() string {
	return fmt.Sprintf("Storage Class %s is forbidden for the current Tenant", f.storageClassName)
}
