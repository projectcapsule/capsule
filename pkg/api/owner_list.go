// Copyright 2020-2025 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package api

import (
	"sort"
)

// +kubebuilder:object:generate=true

type OwnerListSpec []OwnerSpec

func (o OwnerListSpec) IsOwner(name string, groups []string) bool {
	for _, owner := range o {
		switch owner.Kind {
		case UserOwner, ServiceAccountOwner:
			if name == owner.Name {
				return true
			}
		case GroupOwner:
			for _, group := range groups {
				if group == owner.Name {
					return true
				}
			}
		}
	}

	return false
}

func (o OwnerListSpec) ToStatusOwners() OwnerStatusListSpec {
	list := OwnerStatusListSpec{}
	for _, owner := range o {
		list = append(list, owner.CoreOwnerSpec)
	}

	return list
}

func (o OwnerListSpec) FindOwner(name string, kind OwnerKind) (owner OwnerSpec) {
	sort.Sort(ByKindAndName(o))
	i := sort.Search(len(o), func(i int) bool {
		return o[i].Kind >= kind && o[i].Name >= name
	})

	if i < len(o) && o[i].Kind == kind && o[i].Name == name {
		return o[i]
	}

	return owner
}

type ByKindAndName OwnerListSpec

func (b ByKindAndName) Len() int {
	return len(b)
}

func (b ByKindAndName) Less(i, j int) bool {
	if b[i].Kind.String() != b[j].Kind.String() {
		return b[i].Kind.String() < b[j].Kind.String()
	}

	return b[i].Name < b[j].Name
}

func (b ByKindAndName) Swap(i, j int) {
	b[i], b[j] = b[j], b[i]
}
