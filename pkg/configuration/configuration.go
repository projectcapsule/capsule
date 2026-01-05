// Copyright 2020-2025 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package configuration

import (
	"context"
	"regexp"

	"github.com/projectcapsule/capsule/pkg/api"
	capsuleapi "github.com/projectcapsule/capsule/pkg/api"
	"k8s.io/client-go/rest"
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
	Users() capsuleapi.UserListSpec
	GetUsersByStatus() capsuleapi.UserListSpec
	IgnoreUserWithGroups() []string
	ForbiddenUserNodeLabels() *capsuleapi.ForbiddenListSpec
	ForbiddenUserNodeAnnotations() *capsuleapi.ForbiddenListSpec
	Administrators() capsuleapi.UserListSpec
	ServiceAccountClientProperties() *api.ServiceAccountClient
	ServiceAccountClient(context.Context) (*rest.Config, error)
}
