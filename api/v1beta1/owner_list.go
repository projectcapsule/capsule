// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package v1beta1

type OwnerListSpec []OwnerSpec

func (in OwnerListSpec) FindOwner(name string, kind OwnerKind) OwnerSpec {
	for _, owner := range in {
		if owner.Kind == kind && owner.Name == name {
			return owner
		}
	}

	return OwnerSpec{}
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
