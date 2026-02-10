// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package api_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/projectcapsule/capsule/pkg/api"
)

func TestOwnerListSpec_FindOwner(t *testing.T) {
	bla := api.OwnerSpec{
		CoreOwnerSpec: api.CoreOwnerSpec{
			UserSpec: api.UserSpec{
				Kind: api.UserOwner,
				Name: "bla",
			},
		},
		ProxyOperations: []api.ProxySettings{
			{
				Kind:       api.IngressClassesProxy,
				Operations: []api.ProxyOperation{"Delete"},
			},
		},
	}
	bar := api.OwnerSpec{
		CoreOwnerSpec: api.CoreOwnerSpec{
			UserSpec: api.UserSpec{
				Kind: api.GroupOwner,
				Name: "bar",
			},
		},
		ProxyOperations: []api.ProxySettings{
			{
				Kind:       api.StorageClassesProxy,
				Operations: []api.ProxyOperation{"Delete"},
			},
		},
	}
	baz := api.OwnerSpec{
		CoreOwnerSpec: api.CoreOwnerSpec{
			UserSpec: api.UserSpec{
				Kind: api.UserOwner,
				Name: "baz",
			},
		},
		ProxyOperations: []api.ProxySettings{
			{
				Kind:       api.StorageClassesProxy,
				Operations: []api.ProxyOperation{"Update"},
			},
		},
	}
	fim := api.OwnerSpec{
		CoreOwnerSpec: api.CoreOwnerSpec{
			UserSpec: api.UserSpec{
				Kind: api.ServiceAccountOwner,
				Name: "fim",
			},
		},
		ProxyOperations: []api.ProxySettings{
			{
				Kind:       api.NodesProxy,
				Operations: []api.ProxyOperation{"List"},
			},
		},
	}
	bom := api.OwnerSpec{
		CoreOwnerSpec: api.CoreOwnerSpec{
			UserSpec: api.UserSpec{
				Kind: api.GroupOwner,
				Name: "bom",
			},
		},
		ProxyOperations: []api.ProxySettings{
			{
				Kind:       api.StorageClassesProxy,
				Operations: []api.ProxyOperation{"Delete"},
			},
			{
				Kind:       api.NodesProxy,
				Operations: []api.ProxyOperation{"Delete"},
			},
		},
	}
	qip := api.OwnerSpec{
		CoreOwnerSpec: api.CoreOwnerSpec{
			UserSpec: api.UserSpec{
				Kind: api.ServiceAccountOwner,
				Name: "qip",
			},
		},
		ProxyOperations: []api.ProxySettings{
			{
				Kind:       api.StorageClassesProxy,
				Operations: []api.ProxyOperation{"List", "Delete"},
			},
		},
	}
	owners := api.OwnerListSpec{bom, qip, bla, bar, baz, fim}

	assert.Equal(t, owners.FindOwner("bom", api.GroupOwner), bom)
	assert.Equal(t, owners.FindOwner("qip", api.ServiceAccountOwner), qip)
	assert.Equal(t, owners.FindOwner("bla", api.UserOwner), bla)
	assert.Equal(t, owners.FindOwner("bar", api.GroupOwner), bar)
	assert.Equal(t, owners.FindOwner("baz", api.UserOwner), baz)
	assert.Equal(t, owners.FindOwner("fim", api.ServiceAccountOwner), fim)
	assert.Equal(t, owners.FindOwner("notfound", api.ServiceAccountOwner), api.OwnerSpec{})
}
