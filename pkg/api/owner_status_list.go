// Copyright 2020-2025 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package api

import (
	"sort"
)

// +kubebuilder:object:generate=true

type OwnerStatusListSpec []CoreOwnerSpec

func (o *OwnerStatusListSpec) Upsert(
	newOwner CoreOwnerSpec,
) {
	owners := *o

	// Ensure slice is sorted before binary search
	sort.Sort(GetByKindAndName(owners))

	less := func(a, b CoreOwnerSpec) bool {
		if a.Kind.String() != b.Kind.String() {
			return a.Kind.String() < b.Kind.String()
		}

		return a.Name < b.Name
	}

	// Find the first index where owners[i] >= newOwner
	idx := sort.Search(len(owners), func(i int) bool {
		return !less(owners[i], newOwner)
	})

	// If we found an exact match (same Kind + Name), merge ClusterRoles
	if idx < len(owners) && !less(owners[idx], newOwner) && !less(newOwner, owners[idx]) {
		existing := &owners[idx]

		roleSet := make(map[string]struct{}, len(existing.ClusterRoles))
		for _, r := range existing.ClusterRoles {
			roleSet[r] = struct{}{}
		}

		for _, r := range newOwner.ClusterRoles {
			if _, ok := roleSet[r]; !ok {
				existing.ClusterRoles = append(existing.ClusterRoles, r)
				roleSet[r] = struct{}{}
			}
		}

		*o = owners

		return
	}

	// Not found: append and keep sorted
	owners = append(owners, newOwner)
	sort.Sort(GetByKindAndName(owners))
	*o = owners
}

func (o OwnerStatusListSpec) IsOwner(name string, groups []string) bool {
	var groupSet map[string]struct{}
	if len(groups) > 0 {
		groupSet = make(map[string]struct{}, len(groups))
		for _, g := range groups {
			groupSet[g] = struct{}{}
		}
	}

	for _, owner := range o {
		switch owner.Kind {
		case UserOwner, ServiceAccountOwner:
			if name == owner.Name {
				return true
			}
		case GroupOwner:
			if groupSet == nil {
				continue
			}

			if _, ok := groupSet[owner.Name]; ok {
				return true
			}
		}
	}

	return false
}

func (o OwnerStatusListSpec) FindOwner(name string, kind OwnerKind) (CoreOwnerSpec, bool) {
	// Sort in-place by (Kind.String(), Name).
	sort.Sort(GetByKindAndName(o))

	targetKind := kind.String()
	n := len(o)

	idx := sort.Search(n, func(i int) bool {
		ki := o[i].Kind.String()

		switch {
		case ki > targetKind:
			return true
		case ki < targetKind:
			return false
		default:
			return o[i].Name >= name
		}
	})

	if idx < n &&
		o[idx].Kind.String() == targetKind &&
		o[idx].Name == name {
		return o[idx], true
	}

	return CoreOwnerSpec{}, false
}

type GetByKindAndName OwnerStatusListSpec

func (b GetByKindAndName) Len() int {
	return len(b)
}

func (b GetByKindAndName) Less(i, j int) bool {
	if b[i].Kind.String() != b[j].Kind.String() {
		return b[i].Kind.String() < b[j].Kind.String()
	}

	return b[i].Name < b[j].Name
}

func (b GetByKindAndName) Swap(i, j int) {
	b[i], b[j] = b[j], b[i]
}
