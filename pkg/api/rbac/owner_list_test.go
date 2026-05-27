// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package rbac_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/projectcapsule/capsule/pkg/api/rbac"
)

func TestOwnerListSpec_FindOwner(t *testing.T) {
	bla := rbac.OwnerSpec{
		CoreOwnerSpec: rbac.CoreOwnerSpec{
			UserSpec: rbac.UserSpec{
				Kind: rbac.UserOwner,
				Name: "bla",
			},
		},
		ProxyOperations: []rbac.ProxySettings{
			{
				Kind:       rbac.IngressClassesProxy,
				Operations: []rbac.ProxyOperation{"Delete"},
			},
		},
	}
	bar := rbac.OwnerSpec{
		CoreOwnerSpec: rbac.CoreOwnerSpec{
			UserSpec: rbac.UserSpec{
				Kind: rbac.GroupOwner,
				Name: "bar",
			},
		},
		ProxyOperations: []rbac.ProxySettings{
			{
				Kind:       rbac.StorageClassesProxy,
				Operations: []rbac.ProxyOperation{"Delete"},
			},
		},
	}
	baz := rbac.OwnerSpec{
		CoreOwnerSpec: rbac.CoreOwnerSpec{
			UserSpec: rbac.UserSpec{
				Kind: rbac.UserOwner,
				Name: "baz",
			},
		},
		ProxyOperations: []rbac.ProxySettings{
			{
				Kind:       rbac.StorageClassesProxy,
				Operations: []rbac.ProxyOperation{"Update"},
			},
		},
	}
	fim := rbac.OwnerSpec{
		CoreOwnerSpec: rbac.CoreOwnerSpec{
			UserSpec: rbac.UserSpec{
				Kind: rbac.ServiceAccountOwner,
				Name: "fim",
			},
		},
		ProxyOperations: []rbac.ProxySettings{
			{
				Kind:       rbac.NodesProxy,
				Operations: []rbac.ProxyOperation{"List"},
			},
		},
	}
	bom := rbac.OwnerSpec{
		CoreOwnerSpec: rbac.CoreOwnerSpec{
			UserSpec: rbac.UserSpec{
				Kind: rbac.GroupOwner,
				Name: "bom",
			},
		},
		ProxyOperations: []rbac.ProxySettings{
			{
				Kind:       rbac.StorageClassesProxy,
				Operations: []rbac.ProxyOperation{"Delete"},
			},
			{
				Kind:       rbac.NodesProxy,
				Operations: []rbac.ProxyOperation{"Delete"},
			},
		},
	}
	qip := rbac.OwnerSpec{
		CoreOwnerSpec: rbac.CoreOwnerSpec{
			UserSpec: rbac.UserSpec{
				Kind: rbac.ServiceAccountOwner,
				Name: "qip",
			},
		},
		ProxyOperations: []rbac.ProxySettings{
			{
				Kind:       rbac.StorageClassesProxy,
				Operations: []rbac.ProxyOperation{"List", "Delete"},
			},
		},
	}
	owners := rbac.OwnerListSpec{bom, qip, bla, bar, baz, fim}

	assert.Equal(t, owners.FindOwner("bom", rbac.GroupOwner), bom)
	assert.Equal(t, owners.FindOwner("qip", rbac.ServiceAccountOwner), qip)
	assert.Equal(t, owners.FindOwner("bla", rbac.UserOwner), bla)
	assert.Equal(t, owners.FindOwner("bar", rbac.GroupOwner), bar)
	assert.Equal(t, owners.FindOwner("baz", rbac.UserOwner), baz)
	assert.Equal(t, owners.FindOwner("fim", rbac.ServiceAccountOwner), fim)
	assert.Equal(t, owners.FindOwner("notfound", rbac.ServiceAccountOwner), rbac.OwnerSpec{})
}
