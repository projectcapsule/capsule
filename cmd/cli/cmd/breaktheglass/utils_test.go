// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package breaktheglass

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/rest"
)

func TestImpersonationOptionsApplyTo(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		options    impersonationOptions
		config     rest.Config
		wantUser   string
		wantGroups []string
		wantError  string
	}{
		{
			name:       "preserves kubeconfig impersonation",
			config:     rest.Config{Impersonate: rest.ImpersonationConfig{UserName: "configured", Groups: []string{"configured-group"}}},
			wantUser:   "configured",
			wantGroups: []string{"configured-group"},
		},
		{
			name:       "command flags override kubeconfig impersonation",
			options:    impersonationOptions{User: "alice", Groups: []string{"developers", "on-call"}},
			config:     rest.Config{Impersonate: rest.ImpersonationConfig{UserName: "configured", Groups: []string{"configured-group"}}},
			wantUser:   "alice",
			wantGroups: []string{"developers", "on-call"},
		},
		{
			name:     "user flag clears kubeconfig impersonation groups",
			options:  impersonationOptions{User: "alice"},
			config:   rest.Config{Impersonate: rest.ImpersonationConfig{UserName: "configured", Groups: []string{"configured-group"}}},
			wantUser: "alice",
		},
		{
			name:       "group flags use kubeconfig impersonated user",
			options:    impersonationOptions{Groups: []string{"developers"}},
			config:     rest.Config{Impersonate: rest.ImpersonationConfig{UserName: "configured", Groups: []string{"configured-group"}}},
			wantUser:   "configured",
			wantGroups: []string{"developers"},
		},
		{
			name:       "groups require an impersonated user",
			options:    impersonationOptions{Groups: []string{"developers"}},
			wantGroups: []string{"developers"},
			wantError:  "--as-group requires --as or an impersonated user in the kubeconfig",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			cfg := tt.config
			err := tt.options.applyTo(&cfg)
			if tt.wantError != "" {
				require.EqualError(t, err, tt.wantError)
			} else {
				require.NoError(t, err)
			}

			assert.Equal(t, tt.wantUser, cfg.Impersonate.UserName)
			assert.Equal(t, tt.wantGroups, cfg.Impersonate.Groups)
		})
	}
}

func TestAccessEntityForConfig(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "configured", accessEntityForConfig(&rest.Config{Username: "configured"}).Name)
	assert.Equal(t, "alice", accessEntityForConfig(&rest.Config{
		Username:    "configured",
		Impersonate: rest.ImpersonationConfig{UserName: "alice"},
	}).Name)
}
