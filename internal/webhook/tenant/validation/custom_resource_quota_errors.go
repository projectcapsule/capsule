// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package validation

import "fmt"

type customResourceQuotaError struct {
	kindGroup string
	limit     int64
}

func NewCustomResourceQuotaError(kindGroup string, limit int64) error {
	return &customResourceQuotaError{
		kindGroup: kindGroup,
		limit:     limit,
	}
}

func (r customResourceQuotaError) Error() string {
	return fmt.Sprintf("resource %s has reached quota limit of %d items", r.kindGroup, r.limit)
}
