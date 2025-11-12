// Copyright 2020-2025 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package configuration

import (
	"context"
	"fmt"
	"os"
	"regexp"

	"github.com/pkg/errors"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api"
)

// capsuleConfiguration is the Capsule Configuration retrieval mode
// using a closure that provides the desired configuration.
type capsuleConfiguration struct {
	retrievalFn func() *capsulev1beta2.CapsuleConfiguration
	rest        *rest.Config
	client      client.Client
}

func NewCapsuleConfiguration(ctx context.Context, client client.Client, rest *rest.Config, name string) Configuration {
	return &capsuleConfiguration{
		client: client,
		rest:   rest,
		retrievalFn: func() *capsulev1beta2.CapsuleConfiguration {
			config := &capsulev1beta2.CapsuleConfiguration{}

			if err := client.Get(ctx, types.NamespacedName{Name: name}, config); err != nil {
				if apierrors.IsNotFound(err) {
					config = &capsulev1beta2.CapsuleConfiguration{
						ObjectMeta: metav1.ObjectMeta{
							Name: name,
						},
						Spec: capsulev1beta2.CapsuleConfigurationSpec{
							UserGroups:                     []string{"projectcapsule.dev"},
							ForceTenantPrefix:              false,
							ProtectedNamespaceRegexpString: "",
						},
					}

					_ = client.Create(ctx, config)

					return config

				}
				panic(errors.Wrap(err, "Cannot retrieve Capsule configuration with name "+name))
			}

			return config
		}}
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
	return c.retrievalFn().Spec.CapsuleResources.MutatingWebhookConfigurationName
}

func (c *capsuleConfiguration) TenantCRDName() string {
	return TenantCRDName
}

func (c *capsuleConfiguration) ValidatingWebhookConfigurationName() (name string) {
	return c.retrievalFn().Spec.CapsuleResources.ValidatingWebhookConfigurationName
}

func (c *capsuleConfiguration) UserGroups() []string {
	return c.retrievalFn().Spec.UserGroups
}

func (c *capsuleConfiguration) UserNames() []string {
	return c.retrievalFn().Spec.UserNames
}

func (c *capsuleConfiguration) IgnoreUserWithGroups() []string {
	return c.retrievalFn().Spec.IgnoreUserWithGroups
}

func (c *capsuleConfiguration) ForbiddenUserNodeLabels() *api.ForbiddenListSpec {
	if c.retrievalFn().Spec.NodeMetadata == nil {
		return nil
	}

	return &c.retrievalFn().Spec.NodeMetadata.ForbiddenLabels
}

func (c *capsuleConfiguration) ForbiddenUserNodeAnnotations() *api.ForbiddenListSpec {
	if c.retrievalFn().Spec.NodeMetadata == nil {
		return nil
	}

	return &c.retrievalFn().Spec.NodeMetadata.ForbiddenAnnotations
}

func (c *capsuleConfiguration) ServiceAccountClientProperties() *api.ServiceAccountClient {
	if c.retrievalFn().Spec.ServiceAccountClient == nil {
		return nil
	}

	return c.retrievalFn().Spec.ServiceAccountClient
}

func (c *capsuleConfiguration) ServiceAccountClient(ctx context.Context) (client *rest.Config, err error) {
	props := c.ServiceAccountClientProperties()

	client = c.rest

	if props == nil {
		return
	}

	if props.Endpoint != "" {
		client.Host = c.rest.Host
	}

	if props.SkipTLSVerify {
		client.TLSClientConfig.Insecure = true
	} else {
		if props.CASecretName != "" {
			namespace := props.CASecretNamespace
			if namespace == "" {
				namespace = os.Getenv("NAMESPACE")
			}

			caData, err := fetchCACertFromSecret(ctx, c.client, namespace, props.CASecretName, props.CASecretKey)
			if err != nil {
				return nil, fmt.Errorf("could not fetch CA cert: %w", err)
			}

			client.TLSClientConfig.CAData = caData
		}
	}

	return client, nil
}
