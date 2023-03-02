// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package configuration

import (
	"context"
	"regexp"

	"github.com/pkg/errors"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	capsulev1beta2 "github.com/clastix/capsule/api/v1beta2"
	capsuleapi "github.com/clastix/capsule/pkg/api"
)

// capsuleConfiguration is the Capsule Configuration retrieval mode
// using a closure that provides the desired configuration.
type capsuleConfiguration struct {
	retrievalFn func() *capsulev1beta2.CapsuleConfiguration
}

func NewCapsuleConfiguration(ctx context.Context, client client.Client, name string) Configuration {
	return &capsuleConfiguration{retrievalFn: func() *capsulev1beta2.CapsuleConfiguration {
		config := &capsulev1beta2.CapsuleConfiguration{}

		if err := client.Get(ctx, types.NamespacedName{Name: name}, config); err != nil {
			if apierrors.IsNotFound(err) {
				return &capsulev1beta2.CapsuleConfiguration{
					Spec: capsulev1beta2.CapsuleConfigurationSpec{
						UserGroups:                     []string{"capsule.clastix.io"},
						ForceTenantPrefix:              false,
						ProtectedNamespaceRegexpString: "",
					},
				}
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
