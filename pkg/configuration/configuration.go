// Copyright 2020-2023 Project Capsule Authors.
// SPDX-License-Identifier: Apache-2.0

package configuration

import (
	"regexp"

	capsuleapi "github.com/projectcapsule/capsule/pkg/api"
)

const (
	TenantCRDName = "tenants.capsule.clastix.io"
)

type Configuration interface {
	ProtectedNamespaceRegexp() (*regexp.Regexp, error)
	ForceTenantPrefix() bool
	// EnableTLSConfiguration enabled the TLS reconciler, responsible for creating CA and TLS certificate required
	// for the CRD conversion and webhooks.
	EnableTLSConfiguration() bool
	TLSSecretName() string
	MutatingWebhookConfigurationName() string
	ValidatingWebhookConfigurationName() string
	TenantCRDName() string
	UserGroups() []string
	ForbiddenUserNodeLabels() *capsuleapi.ForbiddenListSpec
	ForbiddenUserNodeAnnotations() *capsuleapi.ForbiddenListSpec
}
