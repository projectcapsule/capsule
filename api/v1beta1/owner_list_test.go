// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package v1beta1

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestOwnerListSpec_FindOwner(t *testing.T) {
	bla := OwnerSpec{
		Kind: UserOwner,
		Name: "bla",
		ProxyOperations: []ProxySettings{
			{
				Kind:       IngressClassesProxy,
				Operations: []ProxyOperation{"Delete"},
			},
		},
	}
	bar := OwnerSpec{
		Kind: GroupOwner,
		Name: "bar",
		ProxyOperations: []ProxySettings{
			{
				Kind:       StorageClassesProxy,
				Operations: []ProxyOperation{"Delete"},
			},
		},
	}
	baz := OwnerSpec{
		Kind: UserOwner,
		Name: "baz",
		ProxyOperations: []ProxySettings{
			{
				Kind:       StorageClassesProxy,
				Operations: []ProxyOperation{"Update"},
			},
		},
	}
	fim := OwnerSpec{
		Kind: ServiceAccountOwner,
		Name: "fim",
		ProxyOperations: []ProxySettings{
			{
				Kind:       NodesProxy,
				Operations: []ProxyOperation{"List"},
			},
		},
	}
	bom := OwnerSpec{
		Kind: GroupOwner,
		Name: "bom",
		ProxyOperations: []ProxySettings{
			{
				Kind:       StorageClassesProxy,
				Operations: []ProxyOperation{"Delete"},
			},
			{
				Kind:       NodesProxy,
				Operations: []ProxyOperation{"Delete"},
			},
		},
	}
	qip := OwnerSpec{
		Kind: ServiceAccountOwner,
		Name: "qip",
		ProxyOperations: []ProxySettings{
			{
				Kind:       StorageClassesProxy,
				Operations: []ProxyOperation{"List", "Delete"},
			},
		},
	}
	owners := OwnerListSpec{bom, qip, bla, bar, baz, fim}

	assert.Equal(t, owners.FindOwner("bom", GroupOwner), bom)
	assert.Equal(t, owners.FindOwner("qip", ServiceAccountOwner), qip)
	assert.Equal(t, owners.FindOwner("bla", UserOwner), bla)
	assert.Equal(t, owners.FindOwner("bar", GroupOwner), bar)
	assert.Equal(t, owners.FindOwner("baz", UserOwner), baz)
	assert.Equal(t, owners.FindOwner("fim", ServiceAccountOwner), fim)
	assert.Equal(t, owners.FindOwner("notfound", ServiceAccountOwner), OwnerSpec{})
}
