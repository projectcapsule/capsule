// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package configuration

import (
	"k8s.io/apimachinery/pkg/util/sets"
	"regexp"

	capsulev1beta1 "github.com/clastix/capsule/api/v1beta1"
)

const (
	CASecretName                       = "capsule-ca"
	TLSSecretName                      = "capsule-tls"
	MutatingWebhookConfigurationName   = "capsule-mutating-webhook-configuration"
	ValidatingWebhookConfigurationName = "capsule-validating-webhook-configuration"
)

type Configuration interface {
	ProtectedNamespaceRegexp() (*regexp.Regexp, error)
	ForceTenantPrefix() bool
	CASecretName() string
	TLSSecretName() string
	MutatingWebhookConfigurationName() string
	ValidatingWebhookConfigurationName() string
	UserGroups() []string
	IgnoredUserGroupsAnnotations() sets.String
	ForbiddenUserNodeLabels() *capsulev1beta1.ForbiddenListSpec
	ForbiddenUserNodeAnnotations() *capsulev1beta1.ForbiddenListSpec
}
