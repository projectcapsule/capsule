// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package rbac

import (
	"sort"
	"strings"
)

// +kubebuilder:object:generate=true

type PromotionStatusListSpec []PromotionSpec

func (o *PromotionStatusListSpec) Upsert(newPromotion PromotionSpec) {
	newPromotion.ClusterRoles = mergeSortedStrings(nil, newPromotion.ClusterRoles)
	newPromotion.Targets = mergeSortedStrings(nil, newPromotion.Targets)

	promotions := *o

	for i := range promotions {
		if !samePromotionIdentity(promotions[i], newPromotion) {
			continue
		}

		promotions[i].ClusterRoles = mergeSortedStrings(promotions[i].ClusterRoles, newPromotion.ClusterRoles)

		sort.Sort(GetPromotionByKindNameAndTargets(promotions))

		*o = promotions

		return
	}

	promotions = append(promotions, newPromotion)
	sort.Sort(GetPromotionByKindNameAndTargets(promotions))

	*o = promotions
}

func (o PromotionStatusListSpec) FindUser(name string, kind OwnerKind) (PromotionSpec, bool) {
	result := PromotionSpec{
		UserSpec: UserSpec{
			Name: name,
			Kind: kind,
		},
	}

	found := false

	for _, promotion := range o {
		if promotion.Name != name || promotion.Kind != kind {
			continue
		}

		found = true
		result.ClusterRoles = mergeSortedStrings(result.ClusterRoles, promotion.ClusterRoles)
		result.Targets = mergeSortedStrings(result.Targets, promotion.Targets)
	}

	if !found {
		return PromotionSpec{}, false
	}

	return result, true
}

type GetPromotionByKindNameAndTargets PromotionStatusListSpec

func (b GetPromotionByKindNameAndTargets) Len() int {
	return len(b)
}

func (b GetPromotionByKindNameAndTargets) Less(i, j int) bool {
	return promotionLess(b[i], b[j])
}

func (b GetPromotionByKindNameAndTargets) Swap(i, j int) {
	b[i], b[j] = b[j], b[i]
}

func promotionLess(a, b PromotionSpec) bool {
	if a.Kind.String() != b.Kind.String() {
		return a.Kind.String() < b.Kind.String()
	}

	if a.Name != b.Name {
		return a.Name < b.Name
	}

	return stringSliceKey(a.Targets) < stringSliceKey(b.Targets)
}

func samePromotionIdentity(a, b PromotionSpec) bool {
	return a.Kind == b.Kind &&
		a.Name == b.Name &&
		stringSliceKey(a.Targets) == stringSliceKey(b.Targets)
}

func stringSliceKey(values []string) string {
	sorted := mergeSortedStrings(nil, values)

	var key strings.Builder

	for i, value := range sorted {
		if i > 0 {
			key.WriteString("\x00")
		}

		key.WriteString(value)
	}

	return key.String()
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
