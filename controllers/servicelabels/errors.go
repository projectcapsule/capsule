// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package servicelabels

import "fmt"

type NonTenantObjectError struct {
	objectName string
}

func NewNonTenantObject(objectName string) error {
	return &NonTenantObjectError{objectName: objectName}
}

func (n NonTenantObjectError) Error() string {
	return fmt.Sprintf("Skipping labels sync for %s as it doesn't belong to tenant", n.objectName)
}

type NoServicesMetadataError struct {
	objectName string
}

func NewNoServicesMetadata(objectName string) error {
	return &NoServicesMetadataError{objectName: objectName}
}

func (n NoServicesMetadataError) Error() string {
	return fmt.Sprintf("Skipping labels sync for %s because no AdditionalLabels or AdditionalAnnotations presents in Tenant spec", n.objectName)
}
