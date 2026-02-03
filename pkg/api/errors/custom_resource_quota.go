// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package errors

import "fmt"

type CustomResourceQuotaError struct {
	kindGroup string
	limit     int64
}

func NewCustomResourceQuotaError(kindGroup string, limit int64) error {
	return &CustomResourceQuotaError{
		kindGroup: kindGroup,
		limit:     limit,
	}
}

func (r CustomResourceQuotaError) Error() string {
	return fmt.Sprintf("resource %s has reached quota limit of %d items", r.kindGroup, r.limit)
}
