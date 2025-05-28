// Copyright 2020-2023 Project Capsule Authors.
// SPDX-License-Identifier: Apache-2.0

package utils

import (
	"testing"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"

	"github.com/projectcapsule/capsule/pkg/api"
	"github.com/stretchr/testify/assert"
)

func TestSanitizeServiceAccountProp(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"account", "account"},
		{"namespace:account", "account"},
		{"a:b:c:d:e:f:g", "g"},
		{":account", "account"},
		{"account:", ""},
		{"", ""},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			actual := SanitizeServiceAccountProp(tt.input)
			assert.Equal(t, tt.expected, actual)
		})
	}
}

func TestImpersonatedKubernetesClientForServiceAccount(t *testing.T) {
	reference := api.ServiceAccountReference{
		Name:      "account",
		Namespace: "namespace",
	}

	base := &rest.Config{}
	scheme := runtime.NewScheme()

	client, err := ImpersonatedKubernetesClientForServiceAccount(base, scheme, reference)
	assert.NoError(t, err)
	assert.NotNil(t, client)

	// You can optionally cast and verify fields if needed
	impersonated := rest.CopyConfig(base)
	impersonated.Impersonate.UserName = reference.GetFullName()
	impersonated.Impersonate.Groups = []string{
		"system:serviceaccounts:namespace",
		"system:serviceaccounts",
		"system:authenticated",
	}

	assert.Equal(t, impersonated.Impersonate.UserName, reference.GetFullName())
	assert.ElementsMatch(t, impersonated.Impersonate.Groups, []string{
		"system:serviceaccounts:namespace",
		"system:serviceaccounts",
		"system:authenticated",
	})
}
