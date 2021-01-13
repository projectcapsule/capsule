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

package servicelabels

import "fmt"

type NonTenantObject struct {
	objectName string
}

func NewNonTenantObject(objectName string) error {
	return &NonTenantObject{objectName: objectName}
}

func (n NonTenantObject) Error() string {
	return fmt.Sprintf("Skipping labels sync for %s as it doesn't belong to tenant", n.objectName)
}

type NoServicesMetadata struct {
	objectName string
}

func NewNoServicesMetadata(objectName string) error {
	return &NoServicesMetadata{objectName: objectName}
}

func (n NoServicesMetadata) Error() string {
	return fmt.Sprintf("Skipping labels sync for %s because no AdditionalLabels or AdditionalAnnotations presents in Tenant spec", n.objectName)
}
