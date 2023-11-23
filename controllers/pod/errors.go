// Copyright 2020-2023 Project Capsule Authors.
// SPDX-License-Identifier: Apache-2.0

package pod

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

type NoPodMetadataError struct {
	objectName string
}

func NewNoPodMetadata(objectName string) error {
	return &NoPodMetadataError{objectName: objectName}
}

func (n NoPodMetadataError) Error() string {
	return fmt.Sprintf("Skipping labels sync for %s because no AdditionalLabels or AdditionalAnnotations presents in Tenant spec", n.objectName)
}
