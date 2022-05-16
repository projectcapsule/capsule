// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package v1beta1

import (
	"fmt"
	"strconv"
)

const (
	ResourceQuotaAnnotationPrefix = "quota.resources.capsule.clastix.io"
	ResourceUsedAnnotationPrefix  = "used.resources.capsule.clastix.io"
)

func UsedAnnotationForResource(kindGroup string) string {
	return fmt.Sprintf("%s/%s", ResourceUsedAnnotationPrefix, kindGroup)
}

func LimitAnnotationForResource(kindGroup string) string {
	return fmt.Sprintf("%s/%s", ResourceQuotaAnnotationPrefix, kindGroup)
}

func GetUsedResourceFromTenant(tenant Tenant, kindGroup string) (int64, error) {
	usedStr, ok := tenant.GetAnnotations()[UsedAnnotationForResource(kindGroup)]
	if !ok {
		usedStr = "0"
	}

	used, _ := strconv.ParseInt(usedStr, 10, 10)

	return used, nil
}

type NonLimitedResourceError struct {
	kindGroup string
}

func NewNonLimitedResourceError(kindGroup string) *NonLimitedResourceError {
	return &NonLimitedResourceError{kindGroup: kindGroup}
}

func (n NonLimitedResourceError) Error() string {
	return fmt.Sprintf("resource %s is not limited for the current tenant", n.kindGroup)
}

func GetLimitResourceFromTenant(tenant Tenant, kindGroup string) (int64, error) {
	limitStr, ok := tenant.GetAnnotations()[LimitAnnotationForResource(kindGroup)]
	if !ok {
		return 0, NewNonLimitedResourceError(kindGroup)
	}

	limit, err := strconv.ParseInt(limitStr, 10, 10)
	if err != nil {
		return 0, fmt.Errorf("resource %s limit cannot be parsed, %w", kindGroup, err)
	}

	return limit, nil
}
