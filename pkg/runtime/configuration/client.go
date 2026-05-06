// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package configuration

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"time"

	"github.com/pkg/errors"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	capsuleapi "github.com/projectcapsule/capsule/pkg/api"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/api/rbac"
)

// capsuleConfiguration is the Capsule Configuration retrieval mode
// using a closure that provides the desired configuration.
type capsuleConfiguration struct {
	retrievalFn func() *capsulev1beta2.CapsuleConfiguration
	rest        *rest.Config
	client      client.Client
}

func DefaultCapsuleConfiguration() capsulev1beta2.CapsuleConfigurationSpec {
	d, _ := time.ParseDuration("1h")

	return capsulev1beta2.CapsuleConfigurationSpec{
		Users: []rbac.UserSpec{
			{
				Name: "projectcapsule.dev",
				Kind: rbac.GroupOwner,
			},
		},
		CacheInvalidation: metav1.Duration{
			Duration: d,
		},
		RBAC: &capsulev1beta2.RBACConfiguration{
			DeleterClusterRole:     "capsule-namespace-deleter",
			ProvisionerClusterRole: "capsule-namespace-provisioner",
		},
		ForceTenantPrefix:              false,
		ProtectedNamespaceRegexpString: "",
	}
}

func NewCapsuleConfiguration(ctx context.Context, c client.Client, rest *rest.Config, name string) Configuration {
	return &capsuleConfiguration{
		client: c,
		rest:   rest,
		retrievalFn: func() *capsulev1beta2.CapsuleConfiguration {
			cfg := &capsulev1beta2.CapsuleConfiguration{}
			key := types.NamespacedName{Name: name}

			if err := c.Get(ctx, key, cfg); err == nil {
				return cfg
			} else if !apierrors.IsNotFound(err) {
				panic(errors.Wrap(err, "cannot retrieve Capsule configuration with name "+name))
			}

			err := c.Get(ctx, key, cfg)
			if err == nil {
				return cfg
			}

			if !apierrors.IsNotFound(err) {
				panic(errors.Wrap(err, "cannot retrieve Capsule configuration with name "+name))
			}

			cfg = &capsulev1beta2.CapsuleConfiguration{
				ObjectMeta: metav1.ObjectMeta{
					Name: name,
				},
				Spec: DefaultCapsuleConfiguration(),
			}

			if err := c.Create(ctx, cfg); err != nil {
				if apierrors.IsAlreadyExists(err) {
					if err := c.Get(ctx, key, cfg); err != nil {
						panic(errors.Wrap(err, "configuration created concurrently but cannot be retrieved"))
					}

					return cfg
				}

				panic(errors.Wrap(err, "cannot create Capsule configuration with name "+name))
			}

			return cfg
		},
	}
}

func (c *capsuleConfiguration) GetConfigObject() *capsulev1beta2.CapsuleConfiguration {
	return c.retrievalFn()
}

func (c *capsuleConfiguration) ProtectedNamespaceRegexp() (*regexp.Regexp, error) {
	expr := c.retrievalFn().Spec.ProtectedNamespaceRegexpString
	if len(expr) == 0 {
		return nil, nil //nolint:nilnil
	}

	r, err := regexp.Compile(expr)
	if err != nil {
		return nil, errors.Wrap(err, "Cannot compile the protected namespace regexp")
	}

	return r, nil
}

func (c *capsuleConfiguration) ForceTenantPrefix() bool {
	return c.retrievalFn().Spec.ForceTenantPrefix
}

func (c *capsuleConfiguration) TLSSecretName() (name string) {
	return c.retrievalFn().Spec.CapsuleResources.TLSSecretName
}

func (c *capsuleConfiguration) EnableTLSConfiguration() bool {
	return c.retrievalFn().Spec.EnableTLSReconciler
}

func (c *capsuleConfiguration) AllowServiceAccountPromotion() bool {
	return c.retrievalFn().Spec.AllowServiceAccountPromotion
}

func (c *capsuleConfiguration) MutatingWebhookConfigurationName() (name string) {
	return string(c.retrievalFn().Spec.Admission.Mutating.Name)
}

func (c *capsuleConfiguration) TenantCRDName() string {
	return TenantCRDName
}

func (c *capsuleConfiguration) ValidatingWebhookConfigurationName() (name string) {
	return string(c.retrievalFn().Spec.Admission.Validating.Name)
}

//nolint:staticcheck
func (c *capsuleConfiguration) UserGroups() []string {
	return append(c.retrievalFn().Spec.UserGroups, c.retrievalFn().Spec.Users.GetByKinds([]rbac.OwnerKind{rbac.GroupOwner})...)
}

//nolint:staticcheck
func (c *capsuleConfiguration) UserNames() []string {
	return append(c.retrievalFn().Spec.UserNames, c.retrievalFn().Spec.Users.GetByKinds([]rbac.OwnerKind{rbac.UserOwner, rbac.ServiceAccountOwner})...)
}

func (c *capsuleConfiguration) Users() rbac.UserListSpec {
	out := rbac.UserListSpec{}

	for _, user := range c.UserNames() {
		out.Upsert(rbac.UserSpec{
			Kind: rbac.UserOwner,
			Name: user,
		})
	}

	for _, group := range c.UserGroups() {
		out.Upsert(rbac.UserSpec{
			Kind: rbac.GroupOwner,
			Name: group,
		})
	}

	return out
}

func (c *capsuleConfiguration) GetUsersByStatus() rbac.UserListSpec {
	return c.retrievalFn().Status.Users
}

func (c *capsuleConfiguration) IgnoreUserWithGroups() []string {
	return c.retrievalFn().Spec.IgnoreUserWithGroups
}

func (c *capsuleConfiguration) ForbiddenUserNodeLabels() *capsuleapi.ForbiddenListSpec {
	if c.retrievalFn().Spec.NodeMetadata == nil {
		return nil
	}

	return &c.retrievalFn().Spec.NodeMetadata.ForbiddenLabels
}

func (c *capsuleConfiguration) ForbiddenUserNodeAnnotations() *capsuleapi.ForbiddenListSpec {
	if c.retrievalFn().Spec.NodeMetadata == nil {
		return nil
	}

	return &c.retrievalFn().Spec.NodeMetadata.ForbiddenAnnotations
}

func (c *capsuleConfiguration) Administrators() rbac.UserListSpec {
	return c.retrievalFn().Spec.Administrators
}

func (c *capsuleConfiguration) Admission() capsulev1beta2.DynamicAdmission {
	return c.retrievalFn().Spec.Admission
}

func (c *capsuleConfiguration) RBAC() *capsulev1beta2.RBACConfiguration {
	return c.retrievalFn().Spec.RBAC
}

func (c *capsuleConfiguration) CacheInvalidation() metav1.Duration {
	return c.retrievalFn().Spec.CacheInvalidation
}

func (c *capsuleConfiguration) ServiceAccountClientProperties() capsulev1beta2.ServiceAccountClient {
	return c.retrievalFn().Spec.Impersonation
}

func (c *capsuleConfiguration) ServiceAccountClient(ctx context.Context) (*rest.Config, error) {
	props := c.ServiceAccountClientProperties()

	cfg := rest.CopyConfig(c.rest)

	if props.Endpoint != "" {
		cfg.Host = props.Endpoint
	}

	if props.SkipTLSVerify {
		cfg.Insecure = true
		cfg.CAData = nil
		cfg.CAFile = ""

		return cfg, nil
	}

	if props.CASecretName != "" {
		namespace := props.CASecretNamespace
		if namespace == "" {
			namespace = meta.RFC1123SubdomainName(os.Getenv("NAMESPACE"))
		}

		caData, err := fetchCACertFromSecret(
			ctx,
			c.client,
			namespace.String(),
			props.CASecretName.String(),
			props.CASecretKey,
		)
		if err != nil {
			return nil, fmt.Errorf("could not fetch CA cert: %w", err)
		}

		cfg.CAData = caData
		cfg.CAFile = ""
	}

	return cfg, nil
}
