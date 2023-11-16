// Copyright 2020-2023 Project Capsule Authors.
// SPDX-License-Identifier: Apache-2.0

package namespace

type namespaceQuotaExceededError struct{}

func NewNamespaceQuotaExceededError() error {
	return &namespaceQuotaExceededError{}
}

func (namespaceQuotaExceededError) Error() string {
	return "Cannot exceed Namespace quota: please, reach out to the system administrators"
}
