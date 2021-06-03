// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package configuration

import (
	"context"
	"regexp"

	"github.com/pkg/errors"
	machineryerr "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/clastix/capsule/api/v1alpha1"
)

// capsuleConfiguration is the Capsule Configuration retrieval mode
// using a closure that provides the desired configuration.
type capsuleConfiguration struct {
	retrievalFn func() *v1alpha1.CapsuleConfiguration
}

func NewCapsuleConfiguration(client client.Client, name string) Configuration {
	return &capsuleConfiguration{retrievalFn: func() *v1alpha1.CapsuleConfiguration {
		config := &v1alpha1.CapsuleConfiguration{}

		if err := client.Get(context.Background(), types.NamespacedName{Name: name}, config); err != nil {
			if machineryerr.IsNotFound(err) {
				return &v1alpha1.CapsuleConfiguration{
					Spec: v1alpha1.CapsuleConfigurationSpec{
						UserGroups:                           []string{"capsule.clastix.io"},
						ForceTenantPrefix:                    false,
						ProtectedNamespaceRegexpString:       "",
						AllowTenantIngressHostnamesCollision: false,
						AllowIngressHostnameCollision:        true,
					},
				}
			}
			panic(errors.Wrap(err, "Cannot retrieve Capsule configuration with name "+name))
		}

		return config
	}}
}

func (c capsuleConfiguration) AllowIngressHostnameCollision() bool {
	return c.retrievalFn().Spec.AllowIngressHostnameCollision
}

func (c capsuleConfiguration) AllowTenantIngressHostnamesCollision() bool {
	return c.retrievalFn().Spec.AllowTenantIngressHostnamesCollision
}

func (c capsuleConfiguration) ProtectedNamespaceRegexp() (*regexp.Regexp, error) {
	expr := c.retrievalFn().Spec.ProtectedNamespaceRegexpString
	if len(expr) == 0 {
		return nil, nil
	}
	r, err := regexp.Compile(expr)
	if err != nil {
		return nil, errors.Wrap(err, "Cannot compile the protected namespace regexp")
	}

	return r, nil
}

func (c capsuleConfiguration) ForceTenantPrefix() bool {
	return c.retrievalFn().Spec.ForceTenantPrefix
}

func (c capsuleConfiguration) UserGroups() []string {
	return c.retrievalFn().Spec.UserGroups
}
