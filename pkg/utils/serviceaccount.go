// Copyright 2020-2023 Project Capsule Authors.
// SPDX-License-Identifier: Apache-2.0

package utils

import (
	"fmt"
	"strings"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apiserver/pkg/authentication/serviceaccount"
	"k8s.io/apiserver/pkg/authentication/user"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Returns a namespaced serviceaccount name
func PrivilegedServiceAccountName(name string) (string, error) {
	if _, _, err := serviceaccount.SplitUsername(name); err != nil {
		return "", err
	}

	return name, nil
}

// Returns a namespaced serviceaccount name
func NamespacedServiceAccountName(name string, namespace string) string {
	sanitized := strings.ReplaceAll(name, ":", "")

	return fmt.Sprintf("system:serviceaccount:%s:%s", namespace, sanitized)
}

// Gather all groups for a ServiceAccount
func ServiceAccountGroups(sa string) (groups []string, err error) {
	if namespace, _, err := serviceaccount.SplitUsername(sa); err == nil {
		groups = append(groups, fmt.Sprintf("%s%s", serviceaccount.ServiceAccountGroupPrefix, namespace))
		groups = append(groups, serviceaccount.AllServiceAccountsGroup)
		groups = append(groups, user.AllAuthenticated)
	}

	return
}

// ImpersonatedKubernetesClientForServiceAccount returns a controller-runtime client.Client that impersonates a given ServiceAccount.
func ImpersonatedKubernetesClientForServiceAccount(
	base *rest.Config,
	scheme *runtime.Scheme,
	serviceAccountName string,
) (client.Client, error) {
	groups, err := ServiceAccountGroups(serviceAccountName)
	if err != nil {
		return nil, fmt.Errorf("failed to get service account groups: %w", err)
	}

	impersonated := rest.CopyConfig(base)
	impersonated.Impersonate = rest.ImpersonationConfig{
		UserName: serviceAccountName,
		Groups:   groups,
	}

	k8sClient, err := client.New(impersonated, client.Options{Scheme: scheme})
	if err != nil {
		return nil, fmt.Errorf("failed to create impersonated client: %w", err)
	}

	return k8sClient, nil
}
