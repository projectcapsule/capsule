// Copyright 2020-2023 Project Capsule Authors.
// SPDX-License-Identifier: Apache-2.0

package api

type ServiceAccountClient struct {
	// Kubernetes API Endpoint to use for impersonation
	Endpoint string `json:"endpoint,omitempty"`

	// Namespace where the CA certificate secret is located
	CASecretNamespace string `json:"caSecretNamespace,omitempty"`

	// Name of the secret containing the CA certificate
	CASecretName string `json:"caSecretName,omitempty"`

	// Key in the secret that holds the CA certificate (e.g., "ca.crt")
	// +kubebuilder:default=ca.crt
	CASecretKey string `json:"caSecretKey,omitempty"`

	// If true, TLS certificate verification is skipped (not recommended for production)
	// +kubebuilder:default=false
	SkipTLSVerify bool `json:"skipTlsVerify,omitempty"`

	// Default ServiceAccount for namespaced resources (GlobalTenantResource)
	// When defined, users are required to use this ServiceAccount anywhere in the cluster
	// unless they explicitly provide their own.
	GlobalDefaultServiceAccount string `json:"globalDefaultServiceAccount,omitempty"`

	// Default ServiceAccount for namespaced resources (TenantResource)
	// When defined, users are required to use this ServiceAccount within the namespace
	// where they deploy the resource, unless they explicitly provide their own.
	TenantDefaultServiceAccount string `json:"tenantDefaultServiceAccount,omitempty"`
}
