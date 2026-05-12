// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package rbac

import (
	"sort"
)

// +kubebuilder:object:generate=true

type PromotionStatusListSpec []PromotionSpec

func (o *PromotionStatusListSpec) Upsert(
	newPromotion PromotionSpec,
) {
	promotions := *o

	sort.Sort(GetPromotionByKindAndName(promotions))

	idx := sort.Search(len(promotions), func(i int) bool {
		return !promotionLess(promotions[i], newPromotion)
	})

	if idx < len(promotions) && promotionEqual(promotions[idx], newPromotion) {
		existing := &promotions[idx]

		sort.Strings(existing.ClusterRoles)

		*o = promotions

		return
	}

	promotions = append(promotions, newPromotion)
	sort.Sort(GetPromotionByKindAndName(promotions))

	*o = promotions
}

func (o PromotionStatusListSpec) IsOwner(name string, groups []string) bool {
	var groupSet map[string]struct{}

	if len(groups) > 0 {
		groupSet = make(map[string]struct{}, len(groups))

		for _, group := range groups {
			groupSet[group] = struct{}{}
		}
	}

	for _, owner := range o {
		switch owner.Kind {
		case UserOwner, ServiceAccountOwner:
			if owner.Name == name {
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

//nolint:dupl
func (o PromotionStatusListSpec) FindUser(name string, kind OwnerKind) (PromotionSpec, bool) {
	sort.Sort(GetPromotionByKindAndName(o))

	target := PromotionSpec{
		UserSpec: UserSpec{
			Name: name,
			Kind: kind,
		},
	}

	idx := sort.Search(len(o), func(i int) bool {
		return !promotionLess(o[i], target)
	})

	if idx < len(o) && promotionEqual(o[idx], target) {
		return o[idx], true
	}

	return PromotionSpec{}, false
}

type GetPromotionByKindAndName PromotionStatusListSpec

func (b GetPromotionByKindAndName) Len() int {
	return len(b)
}

func (b GetPromotionByKindAndName) Less(i, j int) bool {
	return promotionLess(b[i], b[j])
}

func (b GetPromotionByKindAndName) Swap(i, j int) {
	b[i], b[j] = b[j], b[i]
}

func promotionLess(a, b PromotionSpec) bool {
	if a.Kind.String() != b.Kind.String() {
		return a.Kind.String() < b.Kind.String()
	}

	return a.Name < b.Name
}

func promotionEqual(a, b PromotionSpec) bool {
	return !promotionLess(a, b) && !promotionLess(b, a)
}

func mergeSortedStrings(existing []string, incoming []string) []string {
	if len(existing) == 0 && len(incoming) == 0 {
		return nil
	}

	values := make(map[string]struct{}, len(existing)+len(incoming))

	for _, value := range existing {
		values[value] = struct{}{}
	}

	for _, value := range incoming {
		values[value] = struct{}{}
	}

	merged := make([]string, 0, len(values))
	for value := range values {
		merged = append(merged, value)
	}

	sort.Strings(merged)

	return merged
}
