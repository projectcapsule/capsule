// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package configuration

import (
	"regexp"

	capsulev1beta1 "github.com/clastix/capsule/api/v1beta1"
)

const (
	TLSSecretName                      = "capsule-tls"
	MutatingWebhookConfigurationName   = "capsule-mutating-webhook-configuration"
	ValidatingWebhookConfigurationName = "capsule-validating-webhook-configuration"
	TenantCRDName                      = "tenants.capsule.clastix.io"
)

type Configuration interface {
	ProtectedNamespaceRegexp() (*regexp.Regexp, error)
	ForceTenantPrefix() bool
	GenerateCertificates() bool
	TLSSecretName() string
	MutatingWebhookConfigurationName() string
	ValidatingWebhookConfigurationName() string
	TenantCRDName() string
	UserGroups() []string
	ForbiddenUserNodeLabels() *capsulev1beta1.ForbiddenListSpec
	ForbiddenUserNodeAnnotations() *capsulev1beta1.ForbiddenListSpec
}
