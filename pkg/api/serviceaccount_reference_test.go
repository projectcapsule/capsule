// Copyright 2020-2025 Project Capsule Authors.
// SPDX-License-Identifier: Apache-2.0

package api_test

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"k8s.io/apiserver/pkg/authentication/serviceaccount"
	"k8s.io/apiserver/pkg/authentication/user"

	"github.com/projectcapsule/capsule/pkg/api"
)

func TestServiceAccountReference_GetFullName(t *testing.T) {
	ref := api.ServiceAccountReference{
		Name:      api.Name("my-sa"),
		Namespace: api.Name("my-ns"),
	}

	expected := fmt.Sprintf("%smy-ns:my-sa", serviceaccount.ServiceAccountUsernamePrefix)
	assert.Equal(t, expected, ref.GetFullName())
}

func TestServiceAccountReference_GetAttributes_Success(t *testing.T) {
	ref := api.ServiceAccountReference{
		Name:      api.Name("my-sa"),
		Namespace: api.Name("my-ns"),
	}

	name, namespace, groups, err := ref.GetAttributes()
	assert.NoError(t, err)
	assert.Equal(t, "my-sa", name)
	assert.Equal(t, "my-ns", namespace)
	assert.Contains(t, groups, serviceaccount.ServiceAccountGroupPrefix+"my-ns")
	assert.Contains(t, groups, serviceaccount.AllServiceAccountsGroup)
	assert.Contains(t, groups, user.AllAuthenticated)
}

func TestServiceAccountReference_GetAttributes_Invalid(t *testing.T) {
	// Invalid because name or namespace is empty
	ref := api.ServiceAccountReference{
		Name:      api.Name(""),
		Namespace: api.Name(""),
	}

	name, namespace, groups, err := ref.GetAttributes()
	assert.Error(t, err)
	assert.Empty(t, name)
	assert.Empty(t, namespace)
	assert.Empty(t, groups)
}
