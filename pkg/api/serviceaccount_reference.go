// Copyright 2020-2025 Project Capsule Authors.
// SPDX-License-Identifier: Apache-2.0

package api

import (
	"fmt"

	"k8s.io/apiserver/pkg/authentication/serviceaccount"
	"k8s.io/apiserver/pkg/authentication/user"
)

// +kubebuilder:object:generate=true
type ServiceAccountReference struct {
	// ServiceAccount Name Reference
	Name Name `json:"name,omitempty"`
	// ServiceAccount Namespace Reference
	Namespace Name `json:"namespace,omitempty"`
}

// GetMatchingNamespaces retrieves the list of namespaces that match the NamespaceSelector.
func (s *ServiceAccountReference) GetFullName() string {
	return fmt.Sprintf("%s%s:%s", serviceaccount.ServiceAccountUsernamePrefix, s.Namespace, s.Name)
}

func (s *ServiceAccountReference) GetAttributes() (name string, namespace string, groups []string, err error) {
	namespace, name, err = serviceaccount.SplitUsername(s.GetFullName())
	if err == nil {
		groups = append(groups, fmt.Sprintf("%s%s", serviceaccount.ServiceAccountGroupPrefix, namespace))
		groups = append(groups, serviceaccount.AllServiceAccountsGroup)
		groups = append(groups, user.AllAuthenticated)
	}

	return
}
