// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package v1beta1

import (
	"sort"
)

type OwnerListSpec []OwnerSpec

func (in OwnerListSpec) FindOwner(name string, kind OwnerKind) (owner OwnerSpec) {
	sort.Sort(ByKindAndName(in))
	i := sort.Search(len(in), func(i int) bool {
		return in[i].Kind >= kind && in[i].Name >= name
	})

	if i < len(in) && in[i].Kind == kind && in[i].Name == name {
		return in[i]
	}

	return
}

type ByKindAndName OwnerListSpec

func (in ByKindAndName) Len() int {
	return len(in)
}

func (in ByKindAndName) Less(i, j int) bool {
	if in[i].Kind.String() != in[j].Kind.String() {
		return in[i].Kind.String() < in[j].Kind.String()
	}

	return in[i].Name < in[j].Name
}

func (in ByKindAndName) Swap(i, j int) {
	in[i], in[j] = in[j], in[i]
}
