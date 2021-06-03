// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package configuration

import (
	"regexp"
)

type Configuration interface {
	AllowIngressHostnameCollision() bool
	AllowTenantIngressHostnamesCollision() bool
	ProtectedNamespaceRegexp() (*regexp.Regexp, error)
	ForceTenantPrefix() bool
	UserGroups() []string
}
