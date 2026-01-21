// Copyright 2020-2025 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package errors

type NamespaceQuotaExceededError struct{}

func NewNamespaceQuotaExceededError() error {
	return &NamespaceQuotaExceededError{}
}

func (NamespaceQuotaExceededError) Error() string {
	return "Cannot exceed Namespace quota: please, reach out to the system administrators"
}
