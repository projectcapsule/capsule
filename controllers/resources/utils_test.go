// Copyright 2020-2023 Project Capsule Authors.
// SPDX-License-Identifier: Apache-2.0

package resources

import (
	"os"
	"testing"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api"
	"github.com/stretchr/testify/assert"
)

type mockConfig struct {
	props *api.ServiceAccountClient
}

func (m *mockConfig) ServiceAccountClientProperties() *api.ServiceAccountClient {
	return m.props
}

func TestSetGlobalTenantResourceServiceAccount(t *testing.T) {
	// Clear env to avoid side effects
	_ = os.Unsetenv("NAMESPACE")

	t.Run("Should sanitize malformed name", func(t *testing.T) {
		resource := &capsulev1beta2.GlobalTenantResource{}
		resource.Spec.ServiceAccount.Name = "invalid:name"

		cfg := &mockConfig{props: &api.ServiceAccountClient{}}

		changed := SetGlobalTenantResourceServiceAccount(cfg, resource)
		assert.True(t, changed)
		assert.Equal(t, "name", resource.Spec.ServiceAccount.Name.String())
	})

	t.Run("Should not change if everything is valid", func(t *testing.T) {
		resource := &capsulev1beta2.GlobalTenantResource{}
		resource.Spec.ServiceAccount.Name = "valid"
		resource.Spec.ServiceAccount.Namespace = "validns"

		changed := SetGlobalTenantResourceServiceAccount(&mockConfig{}, resource)
		assert.False(t, changed)
	})

	t.Run("Should set default namespace from env if empty", func(t *testing.T) {
		_ = os.Setenv("NAMESPACE", "myns")
		resource := &capsulev1beta2.GlobalTenantResource{}
		resource.Spec.ServiceAccount.Name = "sa"

		changed := SetGlobalTenantResourceServiceAccount(&mockConfig{}, resource)
		assert.True(t, changed)
		assert.Equal(t, "myns", resource.Spec.ServiceAccount.Namespace.String())
	})
}

func TestSetTenantResourceServiceAccount(t *testing.T) {
	t.Run("Should sanitize name and set namespace from resource", func(t *testing.T) {
		resource := &capsulev1beta2.TenantResource{}
		resource.Spec.ServiceAccount.Name = "some:sa"
		resource.Namespace = "tenant:ns"

		changed := SetTenantResourceServiceAccount(&mockConfig{}, resource)
		assert.True(t, changed)
		assert.Equal(t, "sa", resource.Spec.ServiceAccount.Name.String())
		assert.Equal(t, "tenantns", resource.Spec.ServiceAccount.Namespace.String())
	})

	t.Run("Should not change if all values already valid", func(t *testing.T) {
		resource := &capsulev1beta2.TenantResource{}
		resource.Spec.ServiceAccount.Name = "sa"
		resource.Spec.ServiceAccount.Namespace = "ns"
		resource.Namespace = "ns"

		changed := SetTenantResourceServiceAccount(&mockConfig{}, resource)
		assert.False(t, changed)
	})
}
