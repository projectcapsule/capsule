// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package errors

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
