// Copyright 2020-2025 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package configuration

import (
	"context"
	"regexp"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
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
	ServiceAccountClientProperties() capsulev1beta2.ServiceAccountClient
	RBAC() *capsulev1beta2.RbacConfiguration
	ServiceAccountClient(context.Context) (*rest.Config, error)
	CacheInvalidation() metav1.Duration
}
