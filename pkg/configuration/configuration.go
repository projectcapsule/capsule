// Copyright 2020-2025 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package configuration

import (
	"context"
	"regexp"

	"k8s.io/client-go/rest"

	"github.com/projectcapsule/capsule/pkg/api"
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
	AllowServiceAccountPromotion() bool
	TLSSecretName() string
	MutatingWebhookConfigurationName() string
	ValidatingWebhookConfigurationName() string
	TenantCRDName() string
	UserNames() []string
	UserGroups() []string
	IgnoreUserWithGroups() []string
	ForbiddenUserNodeLabels() *api.ForbiddenListSpec
	ForbiddenUserNodeAnnotations() *api.ForbiddenListSpec
	ServiceAccountClientProperties() *api.ServiceAccountClient
	ServiceAccountClient(context.Context) (*rest.Config, error)
}
