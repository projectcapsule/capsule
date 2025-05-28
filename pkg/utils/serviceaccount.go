// Copyright 2020-2023 Project Capsule Authors.
// SPDX-License-Identifier: Apache-2.0

package utils

import (
	"fmt"
	"strings"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/projectcapsule/capsule/pkg/api"
)

// Returns a namespaced serviceaccount name
func SanitizeServiceAccountProp(name string) string {
	parts := strings.Split(name, ":")
	if len(parts) == 1 {
		return name
	}

	return parts[len(parts)-1]
}

// ImpersonatedKubernetesClientForServiceAccount returns a controller-runtime client.Client that impersonates a given ServiceAccount.
func ImpersonatedKubernetesClientForServiceAccount(
	base *rest.Config,
	scheme *runtime.Scheme,
	reference *api.ServiceAccountReference,
) (client.Client, error) {
	_, _, groups, err := reference.GetAttributes()
	if err != nil {
		return nil, fmt.Errorf("failed to get service account groups: %w", err)
	}

	impersonated := rest.CopyConfig(base)
	impersonated.Impersonate = rest.ImpersonationConfig{
		UserName: reference.GetFullName(),
		Groups:   groups,
	}

	k8sClient, err := client.New(impersonated, client.Options{Scheme: scheme})
	if err != nil {
		return nil, fmt.Errorf("failed to create impersonated client: %w", err)
	}

	return k8sClient, nil
}
